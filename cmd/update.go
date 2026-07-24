package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
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

// downloadAssetBytes downloads an entire release asset into memory. Used for
// artifacts that must be fully read before use (e.g. to hash and verify).
func downloadAssetBytes(version, asset string) ([]byte, error) {
	body, err := downloadAsset(version, asset)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}

// parseChecksums parses a sha256sum-style checksums file (one
// "<sha256>  <filename>" line per asset) into a filename → hex digest map.
func parseChecksums(r io.Reader) (map[string]string, error) {
	sums := make(map[string]string)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// last field is the filename (strip a binary-mode '*' prefix if present)
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		sums[name] = fields[0]
	}
	return sums, sc.Err()
}

// fetchChecksums downloads the release's checksums.txt and returns a
// filename → expected sha256 map.
func fetchChecksums(version string) (map[string]string, error) {
	body, err := downloadAsset(version, "checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("download checksums.txt: %w", err)
	}
	defer body.Close()
	return parseChecksums(body)
}

// verifyChecksum looks up asset in sums and confirms data hashes to the entry.
// A missing entry or a mismatch is a hard error: the whole point of self-update
// is not executing an unverified binary.
func verifyChecksum(asset string, data []byte, sums map[string]string) error {
	want, ok := sums[asset]
	if !ok {
		return fmt.Errorf("no checksum entry for %s in checksums.txt", asset)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", asset, want, got)
	}
	return nil
}

// verifyAssetChecksum confirms data matches the entry for asset in the
// release's checksums.txt.
func verifyAssetChecksum(version, asset string, data []byte) error {
	sums, err := fetchChecksums(version)
	if err != nil {
		return err
	}
	return verifyChecksum(asset, data, sums)
}

// downloadVerified downloads the platform binary for a release version and
// verifies its sha256 against the release's checksums.txt before returning the
// asset name and bytes. Callers must not write the bytes anywhere until this
// returns nil.
func downloadVerified(version string) (asset string, data []byte, err error) {
	asset = platformAsset()
	data, err = downloadAssetBytes(version, asset)
	if err != nil {
		return "", nil, err
	}
	if err := verifyAssetChecksum(version, asset, data); err != nil {
		return "", nil, err
	}
	fmt.Fprintf(os.Stderr, "-> verified %s (sha256 matches checksums.txt)\n", asset)
	return asset, data, nil
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

	_, data, err := downloadVerified(version)
	if err != nil {
		return err
	}

	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".linkedin-jobs-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
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

	_, data, err := downloadVerified(version)
	if err != nil {
		return err
	}

	name := "linkedin-jobs"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	if err := os.WriteFile(target, data, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
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
