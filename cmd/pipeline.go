package cmd

import (
	"fmt"
	"os"
	"strings"

	"linkedin-job-cli/internal/config"
	"linkedin-job-cli/internal/linkedin"
	"linkedin-job-cli/internal/llm"
	"linkedin-job-cli/internal/models"
	"linkedin-job-cli/internal/render"
	"linkedin-job-cli/internal/salary"
)

// ingestOptions controls the shared fetch→filter→summarize→store→display pipeline.
type ingestOptions struct {
	minSalary        float64
	excludeCompanies []string
	remote           bool
	noDetail         bool
	noSummarize      bool
	detailDelay      float64
	jsonOut          bool
}

// ingest runs the pipeline on a batch of job cards and returns the filtered set
// (all fetched jobs are still persisted to the store).
func ingest(jobs []*models.JobPosting, opts ingestOptions) []*models.JobPosting {
	cfg := loadCfg()
	st, err := openStore()
	if err != nil {
		die("failed to open DB: %v", err)
	}
	defer st.Close()

	if len(jobs) == 0 {
		fmt.Fprintln(os.Stderr, "No jobs found.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "Found %d jobs.\n", len(jobs))

	// 1. Details (salary + description)
	if !opts.noDetail {
		fmt.Fprintln(os.Stderr, "Fetching job details (salary + description)…")
		c := linkedin.New(cfg)
		c.FetchDetailsBatch(jobs, opts.detailDelay, func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r  %d/%d", done, total)
		})
		fmt.Fprintln(os.Stderr)
	}

	// 2. Filter
	filtered := filterJobs(jobs, opts)
	before := len(jobs)
	if opts.minSalary > 0 {
		fmt.Fprintf(os.Stderr, "Salary filter ≥ %s: %d/%d passed.\n", money(opts.minSalary), len(filtered), before)
	}

	// 3. Summarize the filtered set
	if !opts.noSummarize && len(filtered) > 0 {
		fmt.Fprintln(os.Stderr, "Summarizing…")
		for _, j := range filtered {
			if j.LLMSummary == "" {
				j.LLMSummary = llm.Summarize(j, cfg)
			}
		}
	}

	// 4. Persist ALL fetched jobs
	saved := 0
	for _, j := range jobs {
		if err := st.Upsert(j); err == nil {
			saved++
		}
	}
	fmt.Fprintf(os.Stderr, "Saved %d jobs to %s.\n", saved, cfg.DBPath)

	// 5. Output the filtered set
	if opts.jsonOut {
		if err := render.AsJSON(os.Stdout, filtered); err != nil {
			die("json output failed: %v", err)
		}
	} else {
		render.Table(os.Stdout, filtered)
	}
	return filtered
}

func filterJobs(jobs []*models.JobPosting, opts ingestOptions) []*models.JobPosting {
	var out []*models.JobPosting
	for _, j := range jobs {
		if opts.minSalary > 0 {
			if j.SalaryMax() < opts.minSalary {
				continue
			}
		}
		if opts.remote && !strings.Contains(strings.ToLower(j.Location+" "+j.RemoteType), "remote") {
			continue
		}
		excluded := false
		for _, ex := range opts.excludeCompanies {
			if ex != "" && strings.Contains(strings.ToLower(j.Company), strings.ToLower(ex)) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		out = append(out, j)
	}
	return out
}

func money(f float64) string {
	return fmt.Sprintf("$%.0f", f)
}

// parseMinSalary parses a --min-salary value ("200k"), defaulting to 0 (no filter).
func parseMinSalary(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := salary.ParseShorthand(s)
	if err != nil {
		die("Invalid salary format %q: use '200k' or '200000'.", s)
	}
	return v
}

// resolveDetailDelay reads the configured delay between detail fetches.
func resolveDetailDelay() float64 {
	return config.Load().DetailDelaySeconds
}

// llmSum summarizes a single job using the current config.
func llmSum(j *models.JobPosting) string {
	return llm.Summarize(j, loadCfg())
}
