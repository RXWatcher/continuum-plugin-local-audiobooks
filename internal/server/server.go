// Package server builds the chi handler for /api/v1 and /admin routes.
package server

import (
	"context"
	"html"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/RXWatcher/silo-plugin-local-audiobooks/internal/store"
)

// EnrichmentQueue is the surface the server needs from metadata.Queue.
// Declared as an interface to avoid an import cycle
// (metadata → store, store → server would cycle).
type EnrichmentQueue interface {
	Enqueue(ctx context.Context, audiobookID string) error
}

// Deps holds the handler's collaborators.
type Deps struct {
	Store        *store.Store
	StandaloneOn bool   // true when serving on the standalone listener
	StreamSecret []byte // shared HMAC for stream-token verification

	// Scan triggers a library scan. Returns the scan_event id. Multiple
	// concurrent calls de-duplicate to the same in-flight id. Nil-safe (the
	// admin handler returns 503 when Scan is nil).
	Scan func(context.Context) (int64, error)

	// MetadataQueue is optional. When non-nil, the /admin/metadata/backfill
	// endpoint enqueues enrichment jobs for all audiobooks lacking one.
	MetadataQueue EnrichmentQueue
}

// Server wraps the chi handler.
type Server struct {
	deps Deps
}

func New(d Deps) *Server { return &Server{deps: d} }

// Handler returns the chi router. When StandaloneOn is true, only file +
// cover endpoints answer; everything else returns 404. All standalone
// content endpoints require a valid stream-token query param (enforced in
// the handlers themselves — see T17).
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	if s.deps.StandaloneOn {
		// audiobook_backend.v1 byte routes only on the standalone listener.
		r.Get("/api/v1/stream/{book_id}/{file_idx}", s.handleStreamStandalone)
		r.Get("/api/v1/cover/{book_id}/{size}", s.handleCoverStandalone)
		r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"code":"not_allowed","message":"only stream/cover are exposed on standalone listener"}}`, http.StatusNotFound)
		}))
		return r
	}
	r.Get("/api/v1/catalog", s.handleListCatalog)
	r.Get("/api/v1/catalog/libraries", s.handleListLibraries)
	r.Get("/api/v1/catalog/search", s.handleSearchCatalog)
	r.Get("/api/v1/catalog/{id}", s.handleGetCatalog)
	r.Get("/api/v1/browse/authors", s.handleBrowseAuthors)
	r.Get("/api/v1/browse/genres", s.handleBrowseGenres)
	r.Get("/api/v1/stream/{book_id}/{file_idx}", s.handleStream)
	r.Get("/api/v1/cover/{book_id}/{size}", s.handleCover)
	r.Get("/api/v1/requests/{externalId}", s.handleRequestsStub)
	r.Get("/admin", s.handleAdminHome)
	r.Get("/admin/", s.handleAdminHome)
	r.Post("/admin/scan", s.handleAdminScan)
	r.Get("/admin/scan/status", s.handleAdminScanStatus)
	r.Get("/admin/library-paths", s.handleAdminListPaths)
	r.Get("/admin/filesystem/browse", handleAdminFilesystemBrowse)
	r.Post("/admin/library-paths", s.handleAdminAddPath)
	r.Delete("/admin/library-paths/{id}", s.handleAdminDeletePath)
	r.Get("/admin/config", s.handleAdminGetConfig)
	r.Put("/admin/config", s.handleAdminPutConfig)
	r.Post("/admin/metadata/backfill", s.handleMetadataBackfill)
	return r
}

func (s *Server) handleAdminHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en" data-theme="` + adminTheme(r) + `">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Local Audiobooks</title><style>` + adminThemeCSS() + `</style></head>
<body>
<main class="shell">
<a class="back" href="/admin/plugins">&larr; Plugins</a>
<header><p class="eyebrow">Audiobook backend</p><h1>Local Audiobooks</h1><p>Local library scanning, metadata enrichment, covers, and file access for the Audiobooks portal.</p></header>
<div id="status-strip" class="status-strip" aria-label="Plugin health"><div class="strip-cell"><span class="strip-dot"></span><strong>Loading…</strong></div></div>
<nav class="tabs" aria-label="Local Audiobooks admin sections">
<button class="tab active" data-tab-target="libraries" type="button">Libraries</button>
<button class="tab" data-tab-target="scans" type="button">Scans</button>
<button class="tab" data-tab-target="metadata" type="button">Metadata</button>
<button class="tab" data-tab-target="diagnostics" type="button">Diagnostics</button>
</nav>
<section class="tab-panel active" id="libraries">
<article class="panel"><div class="panel-head"><div><h2>Library paths</h2><p class="muted">Paths are validated inside the plugin container. Use container-visible mounts such as <code>/mnt/audiobooks</code> or <code>/media/audiobooks</code>.</p></div><span id="path-count" class="badge">0 paths</span></div><form id="path-form" class="row"><input id="path" placeholder="/mnt/audiobooks" aria-label="Container path"><button id="browse-path" type="button">Browse</button><input id="name" placeholder="Display name" aria-label="Display name"><button type="submit">Add path</button></form><div id="path-browser" class="browser" hidden><div class="browser-head"><strong>Browse container paths</strong><button id="close-browser" type="button">Close</button></div><div class="browser-row"><input id="browser-path" value="/" aria-label="Browser path"><button id="open-browser-path" type="button">Open</button><button id="use-browser-path" type="button">Use this folder</button></div><div id="browser-status" class="muted">Choose a folder mounted inside the plugin container.</div><div id="browser-entries" class="browser-entries"></div></div><div id="paths" class="stack muted">Loading paths...</div></article>
</section>
<section class="tab-panel" id="scans">
<article class="panel"><div class="panel-head"><div><h2>Scan operations</h2><p class="muted">Manual scans return an operation id immediately. Active scans are shown below so duplicate triggers are avoided.</p></div><span id="scan-state" class="badge">Loading</span></div><div id="scan-status" class="stack muted">Loading...</div><div class="actions"><button id="scan" type="button">Run scan</button><button id="refresh-status" type="button">Refresh</button></div></article>
</section>
<section class="tab-panel" id="metadata">
<article class="panel"><div class="panel-head"><div><h2>Metadata settings</h2><p class="muted">Source selection, enrichment behavior, cache behavior, and optional standalone streaming.</p></div><button id="save-config" type="button">Save settings</button></div><form id="config-form" class="settings-grid"><label>Enabled sources<input id="metadata-sources" placeholder="audnexus, audimeta, itunes"></label><label>Scan source<input id="metadata-scan-source" placeholder="audnexus"></label><label>Default region<input id="metadata-region" placeholder="us"></label><label>Cache TTL days<input id="metadata-cache-ttl" type="number" min="1"></label><label>Rate limit RPS<input id="metadata-rate-limit" type="number" min="1"></label><label>Standalone listen<input id="standalone-listen" placeholder=":7879"></label><label>Stream signing secret<input id="stream-secret" type="password" placeholder="32-byte base64 secret"></label><label class="check"><input id="scan-inline" type="checkbox"> Inline enrichment on scan</label></form><div id="config-status" class="muted">Loading settings...</div></article>
<article class="panel"><div class="panel-head"><div><h2>Metadata enrichment</h2><p class="muted">Queue a backfill after large imports, provider changes, or when scanned books have weak metadata.</p></div></div><div id="metadata-status" class="muted">No backfill queued from this page yet.</div><div class="actions"><button id="backfill" type="button">Queue metadata backfill</button></div></article>
</section>
<section class="tab-panel" id="diagnostics">
<article class="panel"><div class="panel-head"><div><h2>Readiness diagnostics</h2><p class="muted">Use this before wiring the backend into the Audiobooks portal.</p></div></div><div id="diagnostics-output" class="diagnostic-grid"></div></article>
</section>
<section class="panel"><h2>Operations checklist</h2><ul><li>Mount audiobook folders into the plugin container before adding paths.</li><li>Add at least one enabled path, then run a scan.</li><li>Add this backend as a presentation library in the Audiobooks portal.</li><li>Use metadata backfill after large imports or source changes.</li></ul></section>
</main>
<script>
const scanStatus=document.getElementById("scan-status"), pathsEl=document.getElementById("paths"), diagnosticsEl=document.getElementById("diagnostics-output"), metadataStatus=document.getElementById("metadata-status"), configStatus=document.getElementById("config-status"), scanButton=document.getElementById("scan"), backfillButton=document.getElementById("backfill"), saveConfigButton=document.getElementById("save-config"), pathInput=document.getElementById("path"), pathBrowser=document.getElementById("path-browser"), browserPath=document.getElementById("browser-path"), browserStatus=document.getElementById("browser-status"), browserEntries=document.getElementById("browser-entries");
const hostToken=new URLSearchParams(location.search).get("token")||"";
const adminBase=(location.pathname.match(/^(\/api\/v1\/plugins\/\d+)/)?.[1]||"")+"/admin";
function adminURL(path){return adminBase+path}
function adminHeaders(extra){return Object.assign(hostToken?{Authorization:"Bearer "+hostToken}:{},extra||{})}
function esc(v){return String(v??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]))}
async function json(url,init){const next=Object.assign({},init||{});next.headers=adminHeaders(next.headers);const r=await fetch(url,next);const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d?.error?.message||d?.message||("HTTP "+r.status));return d}
function activateTab(id){document.querySelectorAll(".tab").forEach(b=>b.classList.toggle("active",b.dataset.tabTarget===id));document.querySelectorAll(".tab-panel").forEach(p=>p.classList.toggle("active",p.id===id))}
document.querySelectorAll(".tab").forEach(b=>b.addEventListener("click",()=>activateTab(b.dataset.tabTarget)))
function scanEventHTML(e){const running=!e.FinishedAt;const cls=e.ErrorText?"danger":running?"warning":"success";return '<div class="scan-card '+cls+'"><strong>#'+esc(e.ID)+' '+(running?"Running":"Finished")+'</strong><span>'+esc(e.StartedAt||"")+'</span><small>Added '+esc(e.BooksAdded||0)+' · Changed '+esc(e.BooksChanged||0)+' · Deleted '+esc(e.BooksDeleted||0)+'</small>'+(e.ErrorText?'<pre>'+esc(e.ErrorText)+'</pre>':"")+'</div>'}
function updateDiagnostics(paths, scans){const active=(scans||[]).find(e=>!e.FinishedAt);const last=(scans||[])[0];diagnosticsEl.innerHTML=[["Library paths",(paths||[]).length?String(paths.length)+" configured":"None configured"],["Active scan",active?"#"+active.ID:"None"],["Last scan",last?((last.ErrorText?"Failed":"Completed")+" #"+last.ID):"Never"],["Next step",(paths||[]).length?"Run scan, then configure this backend in Audiobooks":"Add a container-visible path"]].map(([k,v])=>'<div class="diag"><span>'+esc(k)+'</span><strong>'+esc(v)+'</strong></div>').join("");renderStrip([{label:'Paths',ok:(paths||[]).length>0,detail:(paths||[]).length+' configured'},{label:'Last scan',ok:!last||!last.ErrorText,detail:last?(last.ErrorText?'failed: '+last.ErrorText:'completed'):'never run'},{label:'Stream secret',ok:secretConfigured,detail:secretConfigured?'configured':'unset (book streams will fail to sign)'},{label:'Active scan',ok:!active,detail:active?'scan #'+active.ID+' running':'idle'}])}
function renderStrip(cells){const strip=document.getElementById("status-strip");if(!strip)return;strip.replaceChildren();for(const c of cells){const cell=document.createElement("div");cell.className="strip-cell "+(c.ok?"ok":"bad");if(c.detail)cell.title=c.detail;const dot=document.createElement("span");dot.className="strip-dot";const label=document.createElement("strong");label.textContent=c.label;const note=document.createElement("span");note.textContent=c.ok?"OK":"Check";cell.append(dot,label,note);strip.appendChild(cell)}}
let cachedPaths=[], cachedScans=[], secretConfigured=false;
async function loadStatus(){try{const d=await json(adminURL("/scan/status"));cachedScans=d.items||[];const active=cachedScans.find(e=>!e.FinishedAt);document.getElementById("scan-state").textContent=active?"Scan running":"Idle";scanButton.disabled=Boolean(active);scanButton.textContent=active?"Scan running":"Run scan";scanStatus.innerHTML=cachedScans.length?cachedScans.map(scanEventHTML).join(""):"No scans recorded yet.";updateDiagnostics(cachedPaths,cachedScans)}catch(e){scanStatus.textContent=String(e)}}
async function loadPaths(){try{const d=await json(adminURL("/library-paths"));const items=d.items||d.paths||[];cachedPaths=items;document.getElementById("path-count").textContent=items.length+" path"+(items.length===1?"":"s");pathsEl.innerHTML=items.length?items.map(p=>'<div class="path-row"><span><strong>'+esc(p.name||"Audiobooks")+'</strong><br><small>'+esc(p.path)+'</small></span><button data-id="'+esc(p.id)+'" type="button">Delete</button></div>').join(""):"No library paths configured.";pathsEl.querySelectorAll("button[data-id]").forEach(b=>b.addEventListener("click",async()=>{await json(adminURL("/library-paths/"+b.dataset.id),{method:"DELETE"});await loadPaths()}));updateDiagnostics(cachedPaths,cachedScans)}catch(e){pathsEl.textContent=String(e)}}
function readConfigForm(){const body={metadata_sources_enabled:document.getElementById("metadata-sources").value.split(",").map(x=>x.trim()).filter(Boolean),metadata_scan_source:document.getElementById("metadata-scan-source").value.trim(),metadata_default_region:document.getElementById("metadata-region").value.trim(),metadata_cache_ttl_days:Number(document.getElementById("metadata-cache-ttl").value)||30,metadata_rate_limit_rps:Number(document.getElementById("metadata-rate-limit").value)||5,scan_inline_enrich:document.getElementById("scan-inline").checked,standalone_http_listen:document.getElementById("standalone-listen").value.trim()};const ss=document.getElementById("stream-secret").value.trim();if(ss)body.stream_signing_secret=ss;return body}
function writeConfigForm(c){document.getElementById("metadata-sources").value=(c.metadata_sources_enabled||[]).join(", ");document.getElementById("metadata-scan-source").value=c.metadata_scan_source||"audnexus";document.getElementById("metadata-region").value=c.metadata_default_region||"us";document.getElementById("metadata-cache-ttl").value=c.metadata_cache_ttl_days||30;document.getElementById("metadata-rate-limit").value=c.metadata_rate_limit_rps||5;document.getElementById("scan-inline").checked=Boolean(c.scan_inline_enrich);document.getElementById("standalone-listen").value=c.standalone_http_listen||"";/* Never echo secrets back into the form — a populated password input still ships the value in the DOM. Show "configured" state via placeholder and skip-update semantics: empty submit leaves the secret untouched. */ const ss=document.getElementById("stream-secret"); ss.value=""; ss.placeholder=c.stream_signing_secret?"✓ configured (leave blank to keep)":"32-byte base64 secret"; secretConfigured=Boolean(c.stream_signing_secret); updateDiagnostics(cachedPaths,cachedScans)}
async function loadConfig(){try{const c=await json(adminURL("/config"));writeConfigForm(c);configStatus.textContent="Settings loaded"}catch(e){configStatus.textContent=String(e)}}
saveConfigButton.addEventListener("click",async()=>{saveConfigButton.disabled=true;configStatus.textContent="Saving settings...";try{const c=await json(adminURL("/config"),{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify(readConfigForm())});writeConfigForm(c);configStatus.textContent="Settings saved. Standalone listener changes apply after plugin restart."}catch(e){configStatus.textContent=String(e)}finally{saveConfigButton.disabled=false}})
async function browsePath(path){browserStatus.textContent="Loading...";browserEntries.innerHTML="";try{const d=await json(adminURL("/filesystem/browse?"+new URLSearchParams({path:path||"/"}).toString()));browserPath.value=d.path;browserStatus.textContent=d.entries?.length?d.entries.length+" folder"+(d.entries.length===1?"":"s"):"No child folders";const rows=[];if(d.parent&&d.parent!==d.path)rows.push('<button data-path="'+esc(d.parent)+'" type="button">.. <small>'+esc(d.parent)+'</small></button>');for(const e of d.entries||[])rows.push('<button data-path="'+esc(e.path)+'" type="button"><strong>'+esc(e.name)+'</strong><small>'+esc(e.path)+'</small></button>');browserEntries.innerHTML=rows.join("");browserEntries.querySelectorAll("button[data-path]").forEach(b=>b.addEventListener("click",()=>browsePath(b.dataset.path)))}catch(e){browserStatus.textContent=String(e)}}
document.getElementById("browse-path").addEventListener("click",()=>{pathBrowser.hidden=false;browsePath(pathInput.value||"/")})
document.getElementById("close-browser").addEventListener("click",()=>{pathBrowser.hidden=true})
document.getElementById("open-browser-path").addEventListener("click",()=>browsePath(browserPath.value||"/"))
document.getElementById("use-browser-path").addEventListener("click",()=>{pathInput.value=browserPath.value;pathBrowser.hidden=true;pathInput.focus()})
scanButton.addEventListener("click",async()=>{scanButton.disabled=true;scanButton.textContent="Starting...";try{const d=await json(adminURL("/scan"),{method:"POST"});scanStatus.innerHTML='<div class="scan-card warning"><strong>Scan queued #'+esc(d.scan_event_id)+'</strong><span>Refreshing status...</span></div>';await loadStatus()}catch(e){scanStatus.textContent=String(e);scanButton.disabled=false;scanButton.textContent="Run scan"}})
document.getElementById("refresh-status").addEventListener("click",loadStatus)
backfillButton.addEventListener("click",async()=>{backfillButton.disabled=true;metadataStatus.textContent="Queueing metadata backfill...";try{const d=await json(adminURL("/metadata/backfill"),{method:"POST"});metadataStatus.innerHTML='<div class="scan-card success"><strong>Backfill queued</strong><span>'+esc(d.queued||0)+' jobs queued</span></div>'}catch(e){metadataStatus.textContent=String(e)}finally{backfillButton.disabled=false}})
document.getElementById("path-form").addEventListener("submit",async e=>{e.preventDefault();try{await json(adminURL("/library-paths"),{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({path:document.getElementById("path").value,name:document.getElementById("name").value})});e.target.reset();await loadPaths()}catch(err){pathsEl.textContent=String(err)}})
loadStatus();loadPaths();loadConfig();
</script>
</body></html>`))
}

func adminTheme(r *http.Request) string {
	theme := r.Header.Get("X-Silo-Theme")
	if theme == "" {
		theme = r.URL.Query().Get("theme")
	}
	if theme == "" {
		theme = "default"
	}
	return html.EscapeString(theme)
}

func adminThemeCSS() string {
	return `:root{--bg:#141417;--fg:#e8e8ec;--muted:#a1a1aa;--link:#93c5fd;--panel:#1c1c20;--border:#28282e;--input:#101014;--ok:#22c55e;--warn:#f59e0b;--bad:#ef4444}[data-theme="cinema-light"]{--bg:#f7f3ed;--fg:#201c18;--muted:#756b60;--link:#9a3412;--panel:#fffaf3;--border:#ded1c0;--input:#fff}[data-theme="cobalt-studio"]{--bg:#101623;--fg:#eef4ff;--muted:#afc2e2;--link:#60a5fa;--panel:#172033;--border:#2d3f61;--input:#0d1422}[data-theme="oxblood-noir"]{--bg:#170b10;--fg:#fff1f4;--muted:#f0a6b7;--link:#fb7185;--panel:#241018;--border:#4a2230;--input:#12070b}[data-theme="evergreen-studio"]{--bg:#0d1712;--fg:#ecfdf3;--muted:#9bd6b4;--link:#6ee7b7;--panel:#14241b;--border:#2b4b39;--input:#08110d}*{box-sizing:border-box}body{font-family:system-ui,sans-serif;margin:0;line-height:1.5;background:var(--bg);color:var(--fg)}.shell{max-width:1120px;margin:0 auto;padding:28px}.back{display:inline-flex;margin-bottom:12px;color:var(--link);text-decoration:none}.eyebrow{color:var(--muted);text-transform:uppercase;font-size:12px;letter-spacing:.08em}h1{margin:.2rem 0}h2{font-size:16px;margin:0}.tabs{display:flex;gap:8px;flex-wrap:wrap;margin:18px 0}.tab{background:transparent;color:var(--fg);border:1px solid var(--border)}.tab.active{background:var(--link);color:#08111f}.tab-panel{display:none}.tab-panel.active{display:block}.panel{border:1px solid var(--border);background:var(--panel);border-radius:8px;padding:16px;margin-top:16px}.panel-head{display:flex;justify-content:space-between;gap:16px;align-items:flex-start}.row{display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,180px) auto;gap:8px}.settings-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px;margin:12px 0}.settings-grid label{display:grid;gap:5px;color:var(--muted);font-size:13px}.settings-grid .check{display:flex;align-items:center;color:var(--fg);padding-top:20px}.actions{display:flex;gap:8px;margin-top:12px;flex-wrap:wrap}.stack{display:grid;gap:8px}.stack>*+*{margin-top:0}input{min-width:0;background:var(--input);color:var(--fg);border:1px solid var(--border);border-radius:6px;padding:9px}button{background:var(--link);border:0;border-radius:6px;padding:9px 12px;color:#08111f;font-weight:700;cursor:pointer}button:disabled{opacity:.6;cursor:not-allowed}.muted{color:var(--muted)}.badge{border:1px solid var(--border);border-radius:999px;padding:4px 9px;color:var(--muted);white-space:nowrap}.output,.scan-card{overflow:auto;background:var(--input);border:1px solid var(--border);border-radius:6px;padding:10px;color:var(--fg)}.scan-card{display:grid;gap:3px}.scan-card small,.scan-card span{color:var(--muted)}.scan-card.success{border-color:color-mix(in srgb,var(--ok) 45%,var(--border))}.scan-card.warning{border-color:color-mix(in srgb,var(--warn) 55%,var(--border))}.scan-card.danger{border-color:color-mix(in srgb,var(--bad) 55%,var(--border))}.path-row{display:flex;align-items:center;justify-content:space-between;gap:10px;border:1px solid var(--border);border-radius:6px;padding:10px}.path-row small{color:var(--muted)}.browser{margin:12px 0;border:1px solid var(--border);border-radius:8px;background:var(--input);padding:12px}.browser-head,.browser-row{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:8px}.browser-row input{flex:1}.browser-entries{display:grid;max-height:280px;overflow:auto;border:1px solid var(--border);border-radius:6px}.browser-entries button{display:grid;grid-template-columns:180px minmax(0,1fr);gap:8px;border-radius:0;border-bottom:1px solid var(--border);background:transparent;color:var(--fg);text-align:left}.browser-entries small{color:var(--muted);overflow:hidden;text-overflow:ellipsis}.diagnostic-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px}.diag{display:grid;gap:4px;border:1px solid var(--border);border-radius:6px;background:var(--input);padding:12px}.diag span{color:var(--muted);font-size:12px}.diag strong{font-size:18px}code{color:var(--link)}.status-strip{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:8px;margin-top:14px}.strip-cell{display:flex;align-items:center;gap:8px;border:1px solid var(--border);background:var(--panel);border-radius:6px;padding:8px 10px;font-size:12px}.strip-dot{flex:0 0 8px;width:8px;height:8px;border-radius:999px;background:var(--muted)}.strip-cell.ok .strip-dot{background:var(--ok)}.strip-cell.bad .strip-dot{background:var(--bad)}.strip-cell strong{font-weight:600;color:var(--fg)}.strip-cell span{color:var(--muted);margin-left:auto;font-size:11px}@media(max-width:760px){.row,.panel-head,.browser-row{grid-template-columns:1fr;display:grid}.panel-head{gap:8px}.browser-entries button{grid-template-columns:1fr}}`
}
