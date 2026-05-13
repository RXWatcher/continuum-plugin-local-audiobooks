// Command continuum-plugin-audiobooksdb is the plugin entrypoint.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/jackc/pgx/v5/pgxpool"

	publicmanifest "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/grpc/metadataprovider"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/httproutes"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/metadata"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/metadata/sources"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/migrate"
	pluginrt "github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/scanner"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/scheduler"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/server"
	"github.com/ContinuumApp/continuum-plugin-audiobooksdb/internal/store"
)

//go:embed manifest.json
var manifestRaw []byte

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "continuum-plugin-audiobooksdb"})

	manifest, err := publicmanifest.Load(manifestRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
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
		workerPtr        atomic.Pointer[metadata.EnrichmentWorker]
		queuePtr         atomic.Pointer[metadata.Queue]
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
		if c := cfgPtr.Load(); c != nil && c.ScanInlineEnrich {
			if w := workerPtr.Load(); w != nil {
				if drainErr := w.Drain(ctx); drainErr != nil {
					logger.Warn("inline enrichment drain", "err", drainErr)
				}
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

		// Sync configured library_paths into the table.
		for _, path := range cfg.LibraryPaths {
			if _, err := st.UpsertLibraryPath(ctx, path); err != nil {
				logger.Warn("upsert library_path", "path", path, "err", err)
			}
		}

		// Metadata wiring — build queue first so it can be passed to the HTTP server.
		ua := "continuum-audiobooksdb/" + manifest.GetVersion()
		reg := sources.NewRegistry()
		reg.Register(sources.NewAudnexus(ua))
		reg.Register(sources.NewAudiMeta(ua))
		reg.Register(sources.NewITunes(ua))
		reg.Register(sources.NewStorytel(ua))
		reg.Register(sources.NewBookBeat(ua))
		reg.Register(sources.NewAudioteka(ua))
		reg.Register(sources.NewAudiobookCovers(ua))

		ttl := time.Duration(cfg.MetadataCacheTTLDays) * 24 * time.Hour
		cache := metadata.NewCache(p, ttl)
		aggRegAdapter := newAggregatorRegistryAdapter(reg)
		agg := metadata.NewAggregator(aggRegAdapter, cache, cfg.MetadataRateLimitRPS)

		q := metadata.NewQueue(p)
		workerRegAdapter := newWorkerRegistryAdapter(reg)
		worker := metadata.NewEnrichmentWorker(q, st, workerRegAdapter,
			cfg.MetadataScanSource, cfg.MetadataDefaultRegion, logger)

		queuePtr.Store(q)
		workerPtr.Store(worker)

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
		if cfg.StandaloneHTTPListen != "" {
			secret := []byte(cfg.StreamSigningSecret)
			standaloneSrv := server.New(server.Deps{
				Store:        st,
				StandaloneOn: true,
				StreamSecret: secret,
			})
			httpStandalone.SetHandler(standaloneSrv.Handler())
			addr := cfg.StandaloneHTTPListen
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

		// gRPC server reads enabledFn/regionFn at request time so config
		// changes take effect without a restart.
		enabledFn := func() map[string]bool {
			m := map[string]bool{}
			if c := cfgPtr.Load(); c != nil {
				for _, id := range c.MetadataSourcesEnabled {
					m[id] = true
				}
			}
			return m
		}
		regionFn := func() string {
			if c := cfgPtr.Load(); c != nil {
				return c.MetadataDefaultRegion
			}
			return "us"
		}

		metaSrv.Aggregator = agg
		metaSrv.Registry = reg
		metaSrv.Enabled = enabledFn
		metaSrv.Region = regionFn

		logger.Info("configured",
			"library_paths", cfg.LibraryPaths,
			"standalone", cfg.StandaloneHTTPListen != "")
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
