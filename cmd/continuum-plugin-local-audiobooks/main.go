// Command continuum-plugin-local-audiobooks is the plugin entrypoint.
package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	publicmanifest "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/grpc/metadataprovider"
	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/httproutes"
	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/metadata"
	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/metadata/sources"
	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/migrate"
	pluginrt "github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/runtime"
	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/scanner"
	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/scheduler"
	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/server"
	"github.com/RXWatcher/continuum-plugin-local-audiobooks/internal/store"
)

//go:embed manifest.json
var manifestRaw []byte

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "continuum-plugin-local-audiobooks"})

	manifest, err := publicmanifest.Load(manifestRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
		os.Exit(1)
	}
	if err := hydrateRuntimeManifest(manifest); err != nil {
		fmt.Fprintf(os.Stderr, "hydrate manifest: %v\n", err)
		os.Exit(1)
	}

	httpSrv := httproutes.NewServer()        // host-proxy HTTP routes
	httpStandalone := httproutes.NewServer() // standalone listener routes

	var (
		poolPtr          atomic.Pointer[pgxpool.Pool]
		storePtr         atomic.Pointer[store.Store]
		standaloneOnce   sync.Once
		standaloneAddr   atomic.Value // string
		standaloneSrvPtr atomic.Pointer[http.Server]
		cfgPtr           atomic.Pointer[pluginrt.Config]
		appCfgPtr        atomic.Pointer[store.AppConfig]
		workerPtr        atomic.Pointer[metadata.EnrichmentWorker]
		queuePtr         atomic.Pointer[metadata.Queue]
		cachePtr         atomic.Pointer[metadata.Cache]
	)

	scanMu := sync.Mutex{}

	runScan := func(ctx context.Context) (int64, error) {
		scanMu.Lock()
		defer scanMu.Unlock()
		st := storePtr.Load()
		if st == nil {
			return 0, fmt.Errorf("store not configured")
		}
		paths, err := st.ListLibraryPaths(ctx)
		if err != nil {
			return 0, err
		}
		eventID, _ := st.InsertScanEvent(ctx, nil)
		var totalAdded, totalChanged, totalDeleted int
		adapter := &scanner.StoreAdapter{S: st}
		for _, lp := range paths {
			if !lp.Enabled {
				continue
			}
			res, walkErr := scanner.Walk(ctx, adapter, scanner.WalkParams{
				LibraryPathID:   lp.ID,
				Root:            lp.Path,
				EnrichmentQueue: queuePtr.Load(),
			})
			if walkErr != nil {
				_ = st.FinishScanEvent(ctx, eventID, totalAdded, totalChanged, totalDeleted, walkErr.Error())
				return eventID, walkErr
			}
			totalAdded += res.Added
			totalChanged += res.Changed
			totalDeleted += res.Deleted
			_ = st.MarkLibraryScanned(ctx, lp.ID)
		}
		_ = st.FinishScanEvent(ctx, eventID, totalAdded, totalChanged, totalDeleted, "")
		// Inline enrichment: drain the just-enqueued jobs synchronously when
		// scan_inline_enrich is enabled. Best-effort: drain errors are logged
		// but do not fail the scan.
		if c := appCfgPtr.Load(); c != nil && c.ScanInlineEnrich {
			if w := workerPtr.Load(); w != nil {
				if drainErr := w.Drain(ctx); drainErr != nil {
					logger.Warn("inline enrichment drain", "err", drainErr)
				}
			}
		}
		// Evict stale metadata cache entries after every scan. Best-effort:
		// errors are logged at Warn but do not fail the scan.
		if cache := cachePtr.Load(); cache != nil {
			if _, evictErr := cache.EvictExpired(ctx); evictErr != nil {
				logger.Warn("metadata cache eviction", "err", evictErr)
			}
		}
		return eventID, nil
	}

	drainWorker := func(ctx context.Context) error {
		if w := workerPtr.Load(); w != nil {
			return w.Drain(ctx)
		}
		return nil
	}

	tasks := &scheduler.Tasks{ScanFn: runScan, DrainFn: drainWorker}
	schedSrv := scheduler.New(tasks)

	metaSrv := &metadataprovider.Server{}
	configureMetadata := func(p *pgxpool.Pool, st *store.Store, appCfg store.AppConfig) {
		ua := "continuum-local-audiobooks/" + manifest.GetVersion()
		reg := sources.NewRegistry()
		reg.Register(sources.NewAudnexus(ua))
		reg.Register(sources.NewAudiMeta(ua))
		reg.Register(sources.NewITunes(ua))
		reg.Register(sources.NewStorytel(ua))
		reg.Register(sources.NewBookBeat(ua))
		reg.Register(sources.NewAudioteka(ua))
		reg.Register(sources.NewAudiobookCovers(ua))

		ttl := time.Duration(appCfg.MetadataCacheTTLDays) * 24 * time.Hour
		cache := metadata.NewCache(p, ttl)
		cachePtr.Store(cache)
		aggRegAdapter := newAggregatorRegistryAdapter(reg)
		agg := metadata.NewAggregator(aggRegAdapter, cache, appCfg.MetadataRateLimitRPS)

		q := metadata.NewQueue(p)
		workerRegAdapter := newWorkerRegistryAdapter(reg)
		worker := metadata.NewEnrichmentWorker(q, st, workerRegAdapter,
			appCfg.MetadataScanSource, appCfg.MetadataDefaultRegion, logger)

		queuePtr.Store(q)
		workerPtr.Store(worker)

		enabledFn := func() map[string]bool {
			m := map[string]bool{}
			if c := appCfgPtr.Load(); c != nil {
				for _, id := range c.MetadataSourcesEnabled {
					m[id] = true
				}
			}
			return m
		}
		regionFn := func() string {
			if c := appCfgPtr.Load(); c != nil {
				return c.MetadataDefaultRegion
			}
			return "us"
		}

		metaSrv.SetAggregator(agg)
		metaSrv.SetRegistry(reg)
		metaSrv.SetEnabled(enabledFn)
		metaSrv.SetRegion(regionFn)
	}

	rt := pluginrt.New(manifest, func(cfg pluginrt.Config) error {
		ctx := context.Background()

		pcfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("parse db: %w", err)
		}
		if pcfg.MaxConns < 16 {
			pcfg.MaxConns = 16
		}
		p, err := pgxpool.NewWithConfig(ctx, pcfg)
		if err != nil {
			return fmt.Errorf("pgxpool: %w", err)
		}
		if err := migrate.Run(ctx, cfg.DatabaseURL); err != nil {
			p.Close()
			return fmt.Errorf("migrate: %w", err)
		}
		st := store.New(p)
		if _, err := st.ImportLegacyAppConfig(ctx, appConfigFromRuntimeConfig(cfg)); err != nil {
			p.Close()
			return fmt.Errorf("import legacy app config: %w", err)
		}
		appCfg, err := st.GetAppConfig(ctx)
		if err != nil {
			p.Close()
			return fmt.Errorf("load app config: %w", err)
		}
		appCfgPtr.Store(&appCfg)

		// Sync configured library_paths into the table.
		for _, path := range cfg.LibraryPaths {
			if _, err := st.UpsertLibraryPath(ctx, path); err != nil {
				logger.Warn("upsert library_path", "path", path, "err", err)
			}
		}

		configureMetadata(p, st, appCfg)
		q := queuePtr.Load()

		// Host-proxy HTTP server: serves /api/v1/* (catalog/browse/cover/file)
		// and /admin/* (CRUD + scan + metadata backfill).
		srv := server.New(server.Deps{
			Store:         st,
			Scan:          runScan,
			MetadataQueue: q,
		})
		httpSrv.SetHandler(srv.Handler())

		// Standalone listener: only /api/v1/file/{id} and
		// /api/v1/cover/{id}/{size} answer, both stream-token-gated.
		if appCfg.StandaloneHTTPListen != "" {
			secret, err := pluginrt.DecodeStreamSigningSecret(appCfg.StreamSigningSecret)
			if err != nil {
				return fmt.Errorf("decode stream_signing_secret: %w", err)
			}
			standaloneSrv := server.New(server.Deps{
				Store:        st,
				StandaloneOn: true,
				StreamSecret: secret,
			})
			httpStandalone.SetHandler(standaloneSrv.Handler())
			addr := appCfg.StandaloneHTTPListen
			started := false
			standaloneOnce.Do(func() {
				started = true
				standaloneAddr.Store(addr)
				sl := &http.Server{
					Addr:              addr,
					Handler:           httpStandalone,
					ReadHeaderTimeout: 10 * time.Second,
					ReadTimeout:       60 * time.Second,
					WriteTimeout:      120 * time.Second,
					IdleTimeout:       120 * time.Second,
				}
				standaloneSrvPtr.Store(sl)
				go func() {
					logger.Info("standalone http listener starting", "addr", addr)
					if err := sl.ListenAndServe(); err != nil && err != http.ErrServerClosed {
						logger.Error("standalone listener failed", "addr", addr, "err", err)
					}
				}()
			})
			if !started {
				if prev, _ := standaloneAddr.Load().(string); prev != addr {
					logger.Warn("standalone_http_listen changed; restart to apply",
						"current", prev, "requested", addr)
				}
			}
		}

		storePtr.Store(st)
		if old := poolPtr.Swap(p); old != nil {
			old.Close()
		}

		// Capture cfg for closures used by the gRPC server.
		cfgCopy := cfg
		cfgPtr.Store(&cfgCopy)

		logger.Info("configured",
			"library_paths", cfg.LibraryPaths,
			"standalone", appCfg.StandaloneHTTPListen != "")
		return nil
	})

	// Graceful shutdown for the standalone listener.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		if sl := standaloneSrvPtr.Load(); sl != nil {
			logger.Info("draining standalone http listener", "addr", sl.Addr)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = sl.Shutdown(ctx)
		}
	}()

	sdkruntime.Serve(sdkruntime.ServeConfig{
		Logger: logger,
		Servers: sdkruntime.CapabilityServers{
			Runtime:          rt,
			HttpRoutes:       httpSrv,
			ScheduledTask:    schedSrv,
			MetadataProvider: metaSrv,
		},
	})
}

func hydrateRuntimeManifest(manifest *pluginv1.PluginManifest) error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	if len(manifest.GetSupportedPlatforms()) == 0 {
		manifest.SupportedPlatforms = []*pluginv1.SupportedPlatform{{Os: goruntime.GOOS, Arch: goruntime.GOARCH}}
	}
	return nil
}

func appConfigFromRuntimeConfig(cfg pluginrt.Config) store.AppConfig {
	return store.AppConfig{
		MetadataSourcesEnabled: append([]string(nil), cfg.MetadataSourcesEnabled...),
		MetadataDefaultRegion:  cfg.MetadataDefaultRegion,
		MetadataCacheTTLDays:   cfg.MetadataCacheTTLDays,
		MetadataRateLimitRPS:   cfg.MetadataRateLimitRPS,
		ScanInlineEnrich:       cfg.ScanInlineEnrich,
		MetadataScanSource:     cfg.MetadataScanSource,
		StandaloneHTTPListen:   cfg.StandaloneHTTPListen,
		StreamSigningSecret:    cfg.StreamSigningSecret,
	}.WithDefaults()
}
