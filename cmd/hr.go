package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/hr"
	"linkedin-jobs/internal/linkedin"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/render"
)

var hrNoLLM bool

var hrCmd = &cobra.Command{
	Use:   "hr <linkedin-job-url>",
	Short: "Research who to reach out to about a job (best contact + ranked list)",
	Args:  cobra.ExactArgs(1),
	Long: `Research the best person to reach out to about a LinkedIn job posting.

Given a LinkedIn job URL (a /jobs/view/<id>/ link, a currentJobId= link, or a
search/collection URL), it:

  1. Fetches the public job page and extracts the company, its LinkedIn slug,
     and its numeric company id (from the page's "See who you know" links).
  2. Pulls the company's public profile (tagline/about/industry/size).
  3. Asks the LLM to pick the single best contact role (and, when it is
     confident, a named person) and rank alternatives — explaining why — then
     generates ready-to-click LinkedIn people-search URLs scoped to that
     company for each contact.

With no LLM configured it falls back to a deterministic heuristic based on the
job's seniority signals (founding roles -> founders/CTO; manager roles ->
VP/Director; otherwise recruiter first), still with usable search links.

Works anonymously; no LinkedIn session required.

Examples:
  linkedin-jobs hr "https://www.linkedin.com/jobs/view/4435820129/"
  linkedin-jobs hr "https://www.linkedin.com/jobs/search/?currentJobId=4435820129&f_C=105863333"
  linkedin-jobs hr "https://www.linkedin.com/jobs/view/4435820129/" --json
  linkedin-jobs hr "<url>" --no-llm   # heuristic only`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rawURL := args[0]
		c, _ := newClient(true) // session is optional; company/job pages are public

		fmt.Fprintf(os.Stderr, "Fetching job page…\n")
		ctx, err := c.FetchJobContext(rawURL)
		if err != nil {
			die("failed to read job: %v", err)
		}
		fmt.Fprintf(os.Stderr, "  job:   %s @ %s\n", orNA2(ctx.Title), orNA2(ctx.Company))
		if ctx.CompanySlug != "" {
			fmt.Fprintf(os.Stderr, "  slug:  %s   id: %s\n", ctx.CompanySlug, orNA2(ctx.CompanyID))
		}

		var co *linkedin.CompanyProfile
		if ctx.CompanySlug != "" {
			fmt.Fprintf(os.Stderr, "Fetching company profile…\n")
			if p, err := c.FetchCompanyProfile(ctx.CompanySlug); err == nil && p != nil {
				co = p
			}
		}

		var report *hr.Report
		switch {
		case hrNoLLM:
			report = hr.Heuristic(ctx, co)
		default:
			cfg := loadCfg()
			provider, perr := llm.Resolve(cfg)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "LLM unavailable (%v); using heuristic.\n", perr)
				report = hr.Heuristic(ctx, co)
				break
			}
			fmt.Fprintf(os.Stderr, "Researching best contact via %s…\n", provider.Source)
			r, gerr := hr.Generate(ctx, co, provider)
			if gerr != nil {
				fmt.Fprintf(os.Stderr, "LLM call failed (%v); using heuristic.\n", gerr)
				report = hr.Heuristic(ctx, co)
			} else {
				report = r
			}
		}

		if jsonOut {
			if err := render.AsJSON(os.Stdout, report); err != nil {
				die("json output failed: %v", err)
			}
		} else {
			hr.Render(os.Stdout, report)
		}
		return nil
	},
}

func init() {
	hrCmd.Flags().BoolVar(&hrNoLLM, "no-llm", false, "skip the LLM; use the deterministic heuristic only")
	rootCmd.AddCommand(hrCmd)
}
