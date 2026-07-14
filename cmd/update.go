package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/render"
)

const updateRepo = "paputechxyz/linkedin-job-cli"

var updateCheckOnly bool

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the linkedin-jobs CLI to the latest GitHub release",
	Long: `Check GitHub Releases for a newer version and update the running
binary in place. Downloads the matching platform asset and atomically
replaces the current executable.

Use --check to only report whether a newer version is available.`,
	RunE: runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	latest, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}

	latestVer := strings.TrimPrefix(latest.TagName, "v")
	current := Version
	avail := isNewer(latestVer, current)

	if !avail {
		if jsonOut {
			render.AsJSON(os.Stdout, map[string]any{
				"current":          current,
				"latest":           latestVer,
				"update_available": false,
			})
		} else {
			fmt.Printf("linkedin-jobs %s is up to date.\n", current)
		}
		return nil
	}

	if updateCheckOnly {
		if jsonOut {
			render.AsJSON(os.Stdout, map[string]any{
				"current":          current,
				"latest":           latestVer,
				"update_available": true,
				"release_url":      latest.HTMLURL,
			})
		} else {
			fmt.Printf("Update available: %s → %s\n", current, latestVer)
			fmt.Printf("  %s\n", latest.HTMLURL)
		}
		return nil
	}

	if current == "dev" {
		fmt.Fprintln(os.Stderr, "-> current binary is a dev build (go run); installing release to ~/.local/bin instead of replacing it")
		if err := installToLocalBin(latestVer); err != nil {
			return err
		}
	} else {
		if err := selfUpdate(latestVer); err != nil {
			return err
		}
	}

	if jsonOut {
		render.AsJSON(os.Stdout, map[string]any{
			"current":          current,
			"latest":           latestVer,
			"update_available": true,
			"updated":          true,
			"release_url":      latest.HTMLURL,
		})
	} else {
		fmt.Printf("Updated to %s.\n", latestVer)
	}
	return nil
}

func fetchLatestRelease() (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("GitHub API rate-limited; try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func platformAsset() (asset string) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("linkedin-jobs_%s_%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

func downloadAsset(version, asset string) (io.ReadCloser, error) {
	url := fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", updateRepo, version, asset)
	fmt.Fprintf(os.Stderr, "-> downloading %s\n", url)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed (HTTP %d) — asset %s may not exist for this platform", resp.StatusCode, asset)
	}
	return resp.Body, nil
}

// selfUpdate downloads the release binary and atomically replaces the running
// executable. On Unix the rename is atomic; on Windows the old binary is moved
// aside first since a running exe cannot be overwritten.
func selfUpdate(version string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}

	body, err := downloadAsset(version, platformAsset())
	if err != nil {
		return err
	}
	defer body.Close()

	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".linkedin-jobs-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return fmt.Errorf("write download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			return fmt.Errorf("chmod: %w", err)
		}
	}

	if runtime.GOOS == "windows" {
		old := exePath + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exePath, old); err != nil {
			return fmt.Errorf("move old binary aside: %w", err)
		}
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

// installToLocalBin downloads the release binary into ~/.local/bin (or
// $LJ_INSTALL_DIR), used when the running binary is a dev build.
func installToLocalBin(version string) error {
	dir := os.Getenv("LJ_INSTALL_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home dir: %w", err)
		}
		dir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	body, err := downloadAsset(version, platformAsset())
	if err != nil {
		return err
	}
	defer body.Close()

	name := "linkedin-jobs"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return fmt.Errorf("write download: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", target, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(target, 0o755); err != nil {
			return fmt.Errorf("chmod: %w", err)
		}
	}
	fmt.Fprintf(os.Stderr, "-> installed: %s\n", target)
	return nil
}

// isNewer reports whether latest > current using semantic version comparison.
// A current version of "dev" is always considered older.
func isNewer(latest, current string) bool {
	if current == "dev" {
		return true
	}
	la := parseSemver(latest)
	cu := parseSemver(current)
	for i := 0; i < 3; i++ {
		if la[i] > cu[i] {
			return true
		}
		if la[i] < cu[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	var parts [3]int
	v = strings.TrimPrefix(v, "v")
	fields := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(fields); i++ {
		fmt.Sscanf(fields[i], "%d", &parts[i])
	}
	return parts
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "only check for a newer version; don't update")
	rootCmd.AddCommand(updateCmd)
}
