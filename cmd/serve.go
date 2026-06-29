package cmd

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/salary"
	"linkedin-jobs/internal/store"
)

var (
	serveAddr string
	servePort int
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve a read-only web UI to browse all stored jobs",
	Long: `Starts a local web server that lists every stored job with all fields
visible. Long-text fields (description, summaries, company overview, fit reason,
notes) are collapsed by default and expand on click. Each job title links out to
its LinkedIn posting. Supports full-text search, filters, and sorting.

Read-only — no data is written. Binds to localhost only.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()

		tpl, err := template.New("page").Parse(pageHTML)
		if err != nil {
			die("template parse: %v", err)
		}
		ws := &webServer{st: st, tpl: tpl}

		addr := serveAddr
		if addr == "" {
			addr = "127.0.0.1"
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/", ws.handleIndex)
		srv := &http.Server{
			Addr:              fmt.Sprintf("%s:%d", addr, servePort),
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		fmt.Printf("Serving linkedin-jobs on http://%s/  (read-only, localhost)\n", srv.Addr)
		fmt.Println("Press Ctrl+C to stop.")
		if err := srv.ListenAndServe(); err != nil {
			die("server error: %v", err)
		}
		return nil
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", "127.0.0.1", "bind address (defaults to localhost only)")
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "port to serve on")
	rootCmd.AddCommand(serveCmd)
}

// webServer holds the open store and the parsed page template.
type webServer struct {
	st  *store.Store
	tpl *template.Template
}

func (ws *webServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	pd := pageData{F: readForm(q)}
	jobs, mode, qerr := ws.query(q)
	pd.Mode = mode
	if qerr != nil {
		pd.Error = qerr.Error()
	} else {
		pd.Jobs = make([]jobView, 0, len(jobs))
		for _, j := range jobs {
			pd.Jobs = append(pd.Jobs, toJobView(j))
		}
		pd.N = len(pd.Jobs)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ws.tpl.Execute(w, pd); err != nil {
		fmt.Fprintln(os.Stderr, "render error:", err)
	}
}

// query runs either an FTS5 search (when "q" is present) or a filtered List.
// Search is ranked by FTS relevance; the column filters and sort only apply in
// list mode — this mirrors the CLI's split between `query` and `list`.
func (ws *webServer) query(q url.Values) ([]*models.JobPosting, string, error) {
	if term := strings.TrimSpace(q.Get("q")); term != "" {
		jobs, err := ws.st.SearchFTS(ftsExpr([]string{term}, nil), 0)
		return jobs, "search", err
	}
	f := store.Filters{
		Company:         q.Get("company"),
		Title:           q.Get("title"),
		Location:        q.Get("location"),
		Status:          q.Get("status"),
		Source:          q.Get("source"),
		MinSalary:       softSalary(q.Get("min_salary")),
		MinScore:        softInt(q.Get("min_score")),
		Remote:          q.Get("remote") == "1",
		IncludeFiltered: q.Get("include_filtered") == "1",
		SortByScore:     q.Get("sort") != "salary", // default: fit score
	}
	jobs, err := ws.st.List(f)
	return jobs, "list", err
}

type formVals struct {
	Q, Company, Title, Location, Status, Source string
	MinSalary, MinScore                         string
	Sort                                        string
	Remote, IncludeFiltered                     bool
}

func readForm(q url.Values) formVals {
	sort := q.Get("sort")
	if sort == "" {
		sort = "score"
	}
	return formVals{
		Q:               q.Get("q"),
		Company:         q.Get("company"),
		Title:           q.Get("title"),
		Location:        q.Get("location"),
		Status:          q.Get("status"),
		Source:          q.Get("source"),
		MinSalary:       q.Get("min_salary"),
		MinScore:        q.Get("min_score"),
		Sort:            sort,
		Remote:          q.Get("remote") == "1",
		IncludeFiltered: q.Get("include_filtered") == "1",
	}
}

type jobView struct {
	ID, Title, Company, Location, URL string
	Salary, Status, Source, Remote    string
	Score, ScoreClass                 string
	Industry, Seniority, EmpType      string
	CoSize, CoStage, Years, Visa      string
	Founding                          string
	ListedDate, FetchedDate           string
	Description, DescPreview          string
	LLMSummary, Summary               string
	CompanyOverview, FitReason, Notes string
}

func toJobView(j *models.JobPosting) jobView {
	v := jobView{
		ID:              j.ID,
		Title:           j.Title,
		Company:         j.Company,
		Location:        j.Location,
		URL:             j.URL,
		Salary:          j.SalaryDisplay(),
		Status:          j.Status,
		Source:          j.Source,
		Remote:          j.RemoteType,
		Industry:        j.Industry,
		Seniority:       j.Seniority,
		EmpType:         j.EmploymentType,
		CoSize:          j.CompanySizeBand,
		CoStage:         j.CompanyStage,
		Visa:            j.VisaSponsorship,
		Description:     j.Description,
		LLMSummary:      j.LLMSummary,
		Summary:         j.Summary,
		CompanyOverview: j.CompanyOverview,
		FitReason:       j.FitReason,
		Notes:           j.Notes,
	}
	if j.FitScore != nil {
		v.Score = strconv.Itoa(*j.FitScore)
		v.ScoreClass = scoreClass(*j.FitScore)
	}
	if j.YearsExperience != nil {
		v.Years = strconv.Itoa(*j.YearsExperience) + "+"
	}
	if j.IsFoundingRole {
		v.Founding = "Founding"
	}
	if j.ListedAt > 0 {
		v.ListedDate = time.UnixMilli(j.ListedAt).Format("2006-01-02")
	}
	v.FetchedDate = shortDate(j.FetchedAt)
	if v.Description != "" {
		v.DescPreview = preview(v.Description, 200)
	}
	return v
}

func scoreClass(n int) string {
	switch {
	case n >= 70:
		return "score-high"
	case n >= 40:
		return "score-mid"
	default:
		return "score-low"
	}
}

// preview collapses whitespace and truncates to n runes-ish (bytes here).
func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// shortDate renders an RFC3339 fetched_at as a short local timestamp.
func shortDate(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Format("2006-01-02 15:04")
}

// softSalary parses a min-salary value ("200k") without exiting on bad input.
func softSalary(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := salary.ParseShorthand(s)
	if err != nil {
		return 0
	}
	return v
}

func softInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

type pageData struct {
	Jobs  []jobView
	N     int
	F     formVals
	Mode  string
	Error string
}

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>linkedin-jobs · stored jobs</title>
<style>
  :root {
    --bg:#f6f7f9; --card:#fff; --ink:#1f2328; --muted:#6e7781; --line:#d9dee3;
    --accent:#0969da; --high:#1a7f37; --mid:#9a6700; --low:#cf222e;
  }
  * { box-sizing:border-box; }
  body { margin:0; font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif; color:var(--ink); background:var(--bg); }
  header { background:var(--card); border-bottom:1px solid var(--line); padding:14px 20px; }
  header h1 { margin:0; font-size:18px; }
  header .sub { color:var(--muted); font-size:12px; }
  main { max-width:1100px; margin:0 auto; padding:16px 20px 60px; }
  form.filters { background:var(--card); border:1px solid var(--line); border-radius:8px; padding:12px; margin-bottom:16px; display:flex; flex-wrap:wrap; gap:10px; align-items:flex-end; }
  form.filters .field { display:flex; flex-direction:column; gap:3px; }
  form.filters label { font-size:11px; color:var(--muted); text-transform:uppercase; letter-spacing:.03em; }
  input[type=text], input[type=number], select { padding:6px 8px; border:1px solid var(--line); border-radius:6px; font:inherit; background:#fff; color:var(--ink); }
  input[type=text]#q { min-width:220px; }
  .check { display:flex; flex-direction:row; align-items:center; gap:6px; font-size:13px; }
  .check label { font-size:13px; color:var(--ink); text-transform:none; letter-spacing:0; }
  .actions { display:flex; gap:8px; align-items:center; margin-left:auto; }
  button { padding:7px 14px; border:1px solid transparent; border-radius:6px; background:var(--accent); color:#fff; font:inherit; cursor:pointer; }
  .btnlink { padding:7px 14px; border:1px solid var(--line); border-radius:6px; background:#fff; color:var(--ink); font:inherit; text-decoration:none; }
  .note { background:#fff8c5; border:1px solid #e7c365; color:#5d4a09; padding:8px 12px; border-radius:6px; margin-bottom:12px; font-size:13px; }
  .err { background:#ffebe9; border:1px solid #ffcecb; color:#82071e; padding:8px 12px; border-radius:6px; margin-bottom:12px; }
  .err em { color:#82071e; }
  .count { color:var(--muted); margin:4px 0 14px; }
  .empty { color:var(--muted); padding:40px 0; text-align:center; }
  article.job { background:var(--card); border:1px solid var(--line); border-radius:8px; padding:14px 16px; margin-bottom:12px; }
  .job-head { display:flex; justify-content:space-between; align-items:flex-start; gap:12px; }
  .job-head h2 { margin:0; font-size:16px; font-weight:600; }
  .job-head h2 a { color:var(--accent); text-decoration:none; }
  .job-head h2 a:hover { text-decoration:underline; }
  .score { flex:none; padding:2px 9px; border-radius:999px; font-weight:600; font-size:13px; }
  .score-high { background:#dafbe1; color:var(--high); }
  .score-mid { background:#fff8c5; color:var(--mid); }
  .score-low { background:#ffebe9; color:var(--low); }
  .score-none { background:#eaeef2; color:var(--muted); }
  .meta { margin:6px 0; font-size:13px; }
  .meta .sep { color:var(--line); margin:0 6px; }
  .meta .muted { color:var(--muted); }
  .chips { display:flex; flex-wrap:wrap; gap:6px; margin:6px 0; }
  .chip { background:#eaeef2; color:#24292f; border-radius:999px; padding:2px 9px; font-size:12px; }
  .chip.founding { background:#fff1cf; color:#7d4e00; }
  .dates { color:var(--muted); font-size:12px; margin-top:6px; }
  details { margin-top:8px; border:1px solid var(--line); border-radius:6px; padding:6px 10px; background:#fbfcfd; }
  details summary { cursor:pointer; font-weight:500; font-size:13px; }
  details summary em { color:var(--muted); font-weight:400; font-style:normal; }
  .longtext { margin-top:8px; white-space:pre-wrap; word-wrap:break-word; font-size:13px; }
  code { background:#eaeef2; padding:1px 5px; border-radius:4px; font-size:12px; }
  footer { color:var(--muted); font-size:12px; text-align:center; padding:20px; }
</style>
</head>
<body>
<header>
  <h1>linkedin-jobs · stored jobs</h1>
  <div class="sub">read-only local browser</div>
</header>
<main>
  <form class="filters" method="get" action="/">
    <div class="field">
      <label for="q">Search (full-text)</label>
      <input type="text" id="q" name="q" value="{{.F.Q}}" placeholder="staff engineer">
    </div>
    <div class="field">
      <label for="company">Company</label>
      <input type="text" id="company" name="company" value="{{.F.Company}}">
    </div>
    <div class="field">
      <label for="location">Location</label>
      <input type="text" id="location" name="location" value="{{.F.Location}}">
    </div>
    <div class="field">
      <label for="min_salary">Min salary</label>
      <input type="text" id="min_salary" name="min_salary" value="{{.F.MinSalary}}" placeholder="200k">
    </div>
    <div class="field">
      <label for="min_score">Min score</label>
      <input type="number" id="min_score" name="min_score" value="{{.F.MinScore}}" style="width:80px">
    </div>
    <div class="field">
      <label for="status">Status</label>
      <select id="status" name="status">
        <option value="">any</option>
        <option value="new"{{if eq .F.Status "new"}} selected{{end}}>new</option>
        <option value="saved"{{if eq .F.Status "saved"}} selected{{end}}>saved</option>
        <option value="applied"{{if eq .F.Status "applied"}} selected{{end}}>applied</option>
        <option value="rejected"{{if eq .F.Status "rejected"}} selected{{end}}>rejected</option>
        <option value="filtered"{{if eq .F.Status "filtered"}} selected{{end}}>filtered</option>
      </select>
    </div>
    <div class="field">
      <label for="source">Source</label>
      <select id="source" name="source">
        <option value="">any</option>
        <option value="recommended"{{if eq .F.Source "recommended"}} selected{{end}}>recommended</option>
        <option value="search"{{if eq .F.Source "search"}} selected{{end}}>search</option>
      </select>
    </div>
    <div class="field">
      <label for="sort">Sort</label>
      <select id="sort" name="sort">
        <option value="score"{{if eq .F.Sort "score"}} selected{{end}}>fit score</option>
        <option value="salary"{{if eq .F.Sort "salary"}} selected{{end}}>salary</option>
      </select>
    </div>
    <div class="check"><input type="checkbox" id="remote" name="remote" value="1"{{if .F.Remote}} checked{{end}}> <label for="remote">remote only</label></div>
    <div class="check"><input type="checkbox" id="include_filtered" name="include_filtered" value="1"{{if .F.IncludeFiltered}} checked{{end}}> <label for="include_filtered">show filtered</label></div>
    <div class="actions">
      <button type="submit">Apply</button>
      <a href="/" class="btnlink">Clear</a>
    </div>
  </form>

  {{if .Error}}<div class="err">Search error: {{.Error}}<br><em>Tip: wrap multi-word phrases in quotes, e.g. "staff engineer".</em></div>{{end}}
  {{if eq .Mode "search"}}<div class="note">Showing full-text search results ranked by relevance. Column filters and sort are ignored while searching — clear the search box to filter and sort.</div>{{end}}

  <div class="count">{{.N}} job{{if ne .N 1}}s{{end}}{{if eq .Mode "search"}} matching "{{.F.Q}}"{{end}}</div>

  {{if not .Jobs}}
    <div class="empty">No jobs found.{{if not .Error}} Adjust filters or run <code>linkedin-jobs recommended</code> to fetch more.{{end}}</div>
  {{end}}

  {{range .Jobs}}
  <article class="job">
    <div class="job-head">
      <h2><a href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{or .Title "(untitled)"}}</a></h2>
      {{if .Score}}<span class="score {{.ScoreClass}}">{{.Score}}</span>{{else}}<span class="score score-none">—</span>{{end}}
    </div>
    <div class="meta">
      <strong>{{or .Company "—"}}</strong>
      {{if .Location}}<span class="sep">·</span> {{.Location}}{{end}}
      {{if .Salary}}<span class="sep">·</span> <span class="muted">{{.Salary}}</span>{{end}}
      {{if .Remote}}<span class="sep">·</span> <span class="muted">{{.Remote}}</span>{{end}}
      {{if .Status}}<span class="sep">·</span> <span class="muted">{{.Status}}</span>{{end}}
      {{if .Source}}<span class="sep">·</span> <span class="muted">{{.Source}}</span>{{end}}
    </div>
    <div class="chips">
      {{if .Industry}}<span class="chip">{{.Industry}}</span>{{end}}
      {{if .Seniority}}<span class="chip">{{.Seniority}}</span>{{end}}
      {{if .EmpType}}<span class="chip">{{.EmpType}}</span>{{end}}
      {{if .Years}}<span class="chip">{{.Years}} yrs</span>{{end}}
      {{if .CoSize}}<span class="chip">{{.CoSize}}</span>{{end}}
      {{if .CoStage}}<span class="chip">{{.CoStage}}</span>{{end}}
      {{if .Visa}}<span class="chip">visa: {{.Visa}}</span>{{end}}
      {{if .Founding}}<span class="chip founding">{{.Founding}}</span>{{end}}
    </div>
    {{if .LLMSummary}}
    <details>
      <summary>Summary</summary>
      <div class="longtext">{{.LLMSummary}}</div>
    </details>
    {{else if .Summary}}
    <details>
      <summary>Summary (extractive)</summary>
      <div class="longtext">{{.Summary}}</div>
    </details>
    {{end}}
    {{if .Description}}
    <details>
      <summary>Description {{if .DescPreview}}<em>— {{.DescPreview}}</em>{{end}}</summary>
      <div class="longtext">{{.Description}}</div>
    </details>
    {{end}}
    {{if .CompanyOverview}}
    <details>
      <summary>Company overview</summary>
      <div class="longtext">{{.CompanyOverview}}</div>
    </details>
    {{end}}
    {{if .FitReason}}
    <details>
      <summary>Fit reason</summary>
      <div class="longtext">{{.FitReason}}</div>
    </details>
    {{end}}
    {{if .Notes}}
    <details>
      <summary>Notes</summary>
      <div class="longtext">{{.Notes}}</div>
    </details>
    {{end}}
    {{if or .ListedDate .FetchedDate}}
    <div class="dates">{{if .ListedDate}}listed {{.ListedDate}}{{if .FetchedDate}} · {{end}}{{end}}{{if .FetchedDate}}fetched {{.FetchedDate}}{{end}}</div>
    {{end}}
  </article>
  {{end}}

  <footer>Served read-only from SQLite · <code>linkedin-jobs serve</code></footer>
</main>
</body>
</html>`
