package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const (
	VERSION               = "1.0.4"
	GITHUB_REPO           = "abraham-ny/sitemaptool"
	MAX_URLS_PER_SITEMAP  = 50000
	MAX_SITEMAP_SIZE      = 50 * 1024 * 1024 // 50MB
	SITEMAP_NAMESPACE     = "http://www.sitemaps.org/schemas/sitemap/0.9"
)

// Configuration structure
type Config struct {
	OutputDir         string   `json:"output_dir"`
	BaseURL           string   `json:"base_url"`
	SitemapPrefix     string   `json:"sitemap_prefix"`
	PingOnUpdate      bool     `json:"ping_on_update"`
	PingEngines       []string `json:"ping_engines"`
	DefaultChangefreq string   `json:"default_changefreq"`
	DefaultPriority   float64  `json:"default_priority"`
	RespectRobots     bool     `json:"respect_robots"`
	VCSAware          bool     `json:"vcs_aware"`
	RobotsPath        string   `json:"robots_path"`
	CheckUpdates      bool     `json:"check_updates"`
}

// Database structure for tracking sitemaps
type Database struct {
	Sitemaps       []SitemapInfo   `json:"sitemaps"`
	URLHashes      map[string]bool `json:"url_hashes"`
	CurrentSitemap string          `json:"current_sitemap"`
	LastUpdated    time.Time       `json:"last_updated"`
	mu             sync.RWMutex    `json:"-"`
}

type SitemapInfo struct {
	Filename string    `json:"filename"`
	URLCount int       `json:"url_count"`
	LastMod  time.Time `json:"last_modified"`
}

// XML structures for sitemap
type URLSet struct {
	XMLName xml.Name `xml:"urlset"`
	Xmlns   string   `xml:"xmlns,attr"`
	URLs    []URL    `xml:"url"`
}

type URL struct {
	Loc        string  `xml:"loc"`
	LastMod    string  `xml:"lastmod,omitempty"`
	ChangeFreq string  `xml:"changefreq,omitempty"`
	Priority   float64 `xml:"priority,omitempty"`
}

type SitemapIndex struct {
	XMLName  xml.Name       `xml:"sitemapindex"`
	Xmlns    string         `xml:"xmlns,attr"`
	Sitemaps []SitemapEntry `xml:"sitemap"`
}

type SitemapEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// SitemapManager handles all sitemap operations
type SitemapManager struct {
	Config      *Config
	DB          *Database
	ConfigPath  string
	DBPath      string
	RobotsRules map[string]bool
	fileLock    sync.Mutex
}

func NewSitemapManager() (*SitemapManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configDir := filepath.Join(homeDir, ".sitemaptool")
	configPath := filepath.Join(configDir, "config.json")

	sm := &SitemapManager{
		ConfigPath:  configPath,
		RobotsRules: make(map[string]bool),
	}

	if err := sm.LoadConfig(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(sm.Config.OutputDir, 0755); err != nil {
		return nil, err
	}

	sm.DBPath = filepath.Join(sm.Config.OutputDir, ".sitemaptool_db.json")

	if err := sm.LoadDB(); err != nil {
		return nil, err
	}

	if sm.Config.RespectRobots {
		sm.ParseRobotsTxt()
	}

	return sm, nil
}

func (sm *SitemapManager) LoadConfig() error {
	configDir := filepath.Dir(sm.ConfigPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	if _, err := os.Stat(sm.ConfigPath); os.IsNotExist(err) {
		sm.Config = sm.DefaultConfig()
		return sm.SaveConfig()
	}

	data, err := os.ReadFile(sm.ConfigPath)
	if err != nil {
		return err
	}

	sm.Config = &Config{}
	return json.Unmarshal(data, sm.Config)
}

func (sm *SitemapManager) DefaultConfig() *Config {
	return &Config{
		OutputDir:         "./sitemaps",
		BaseURL:           "https://example.com",
		SitemapPrefix:     "sitemap",
		PingOnUpdate:      false,
		PingEngines: []string{
			"https://www.google.com/ping?sitemap=",
			"https://www.bing.com/ping?sitemap=",
		},
		DefaultChangefreq: "weekly",
		DefaultPriority:   0.5,
		RespectRobots:     true,
		VCSAware:          true,
		RobotsPath:        "./robots.txt",
		CheckUpdates:      true,
	}
}

func (sm *SitemapManager) SaveConfig() error {
	data, err := json.MarshalIndent(sm.Config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sm.ConfigPath, data, 0644)
}

func (sm *SitemapManager) LoadDB() error {
	sm.fileLock.Lock()
	defer sm.fileLock.Unlock()

	if _, err := os.Stat(sm.DBPath); os.IsNotExist(err) {
		sm.DB = &Database{
			Sitemaps:    []SitemapInfo{},
			URLHashes:   make(map[string]bool),
			LastUpdated: time.Now(),
		}
		return sm.SaveDB()
	}

	data, err := os.ReadFile(sm.DBPath)
	if err != nil {
		return err
	}

	sm.DB = &Database{}
	if err := json.Unmarshal(data, sm.DB); err != nil {
		return err
	}

	if sm.DB.URLHashes == nil {
		sm.DB.URLHashes = make(map[string]bool)
	}

	return nil
}

func (sm *SitemapManager) SaveDB() error {
	sm.DB.mu.Lock()
	defer sm.DB.mu.Unlock()

	sm.DB.LastUpdated = time.Now()

	data, err := json.MarshalIndent(sm.DB, "", "  ")
	if err != nil {
		return err
	}

	tempFile := sm.DBPath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}

	return os.Rename(tempFile, sm.DBPath)
}

func (sm *SitemapManager) ParseRobotsTxt() error {
	robotsPath := sm.Config.RobotsPath
	if _, err := os.Stat(robotsPath); os.IsNotExist(err) {
		return nil
	}

	file, err := os.Open(robotsPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inUserAgent := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)

		if strings.HasPrefix(lower, "user-agent:") {
			agent := strings.TrimSpace(strings.TrimPrefix(lower, "user-agent:"))
			inUserAgent = (agent == "*" || agent == "sitemaptool")
		} else if inUserAgent && strings.HasPrefix(lower, "disallow:") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "Disallow:"))
			if path != "" {
				sm.RobotsRules[path] = true
			}
		}
	}

	return scanner.Err()
}

func (sm *SitemapManager) IsURLAllowed(url string) bool {
	if !sm.Config.RespectRobots {
		return true
	}

	for disallowPath := range sm.RobotsRules {
		if strings.HasPrefix(url, disallowPath) {
			return false
		}
	}
	return true
}

func (sm *SitemapManager) HashURL(url string) string {
	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:])
}

func (sm *SitemapManager) AddURL(url string, changefreq string, priority float64) error {
	sm.DB.mu.Lock()
	defer sm.DB.mu.Unlock()

	if !sm.IsURLAllowed(url) {
		return fmt.Errorf("URL disallowed by robots.txt: %s", url)
	}

	urlHash := sm.HashURL(url)
	if sm.DB.URLHashes[urlHash] {
		return fmt.Errorf("URL already exists in sitemap: %s", url)
	}

	currentSitemap, err := sm.GetCurrentSitemap()
	if err != nil {
		return err
	}

	urlset, err := sm.LoadSitemap(currentSitemap)
	if err != nil {
		urlset = &URLSet{
			Xmlns: SITEMAP_NAMESPACE,
			URLs:  []URL{},
		}
	}

	newURL := URL{
		Loc:        url,
		LastMod:    time.Now().Format("2006-01-02"),
		ChangeFreq: changefreq,
		Priority:   priority,
	}

	urlset.URLs = append(urlset.URLs, newURL)
	sm.DB.URLHashes[urlHash] = true

	if err := sm.SaveSitemap(currentSitemap, urlset); err != nil {
		return err
	}

	sm.UpdateSitemapInfo(currentSitemap, len(urlset.URLs))

	if err := sm.SaveDB(); err != nil {
		return err
	}

	if err := sm.GenerateSitemapIndex(); err != nil {
		return err
	}

	if sm.Config.PingOnUpdate {
		go sm.PingSearchEngines()
	}

	return nil
}

func (sm *SitemapManager) GetCurrentSitemap() (string, error) {
	if sm.DB.CurrentSitemap == "" || sm.NeedNewSitemap() {
		return sm.CreateNewSitemap()
	}
	return sm.DB.CurrentSitemap, nil
}

func (sm *SitemapManager) NeedNewSitemap() bool {
	if sm.DB.CurrentSitemap == "" {
		return true
	}

	for _, info := range sm.DB.Sitemaps {
		if info.Filename == sm.DB.CurrentSitemap {
			return info.URLCount >= MAX_URLS_PER_SITEMAP
		}
	}

	return false
}

func (sm *SitemapManager) CreateNewSitemap() (string, error) {
	sitemapNum := len(sm.DB.Sitemaps) + 1
	filename := fmt.Sprintf("%s_%d.xml", sm.Config.SitemapPrefix, sitemapNum)

	sm.DB.Sitemaps = append(sm.DB.Sitemaps, SitemapInfo{
		Filename: filename,
		URLCount: 0,
		LastMod:  time.Now(),
	})

	sm.DB.CurrentSitemap = filename

	return filename, nil
}

func (sm *SitemapManager) LoadSitemap(filename string) (*URLSet, error) {
	path := filepath.Join(sm.Config.OutputDir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &URLSet{Xmlns: SITEMAP_NAMESPACE, URLs: []URL{}}, nil
		}
		return nil, err
	}

	urlset := &URLSet{}
	if err := xml.Unmarshal(data, urlset); err != nil {
		return nil, err
	}

	return urlset, nil
}

func (sm *SitemapManager) SaveSitemap(filename string, urlset *URLSet) error {
	sm.fileLock.Lock()
	defer sm.fileLock.Unlock()

	path := filepath.Join(sm.Config.OutputDir, filename)

	output, err := xml.MarshalIndent(urlset, "", "  ")
	if err != nil {
		return err
	}

	xmlData := []byte(xml.Header + string(output))

	tempFile := path + ".tmp"
	if err := os.WriteFile(tempFile, xmlData, 0644); err != nil {
		return err
	}

	return os.Rename(tempFile, path)
}

func (sm *SitemapManager) UpdateSitemapInfo(filename string, urlCount int) {
	for i, info := range sm.DB.Sitemaps {
		if info.Filename == filename {
			sm.DB.Sitemaps[i].URLCount = urlCount
			sm.DB.Sitemaps[i].LastMod = time.Now()
			return
		}
	}
}

func (sm *SitemapManager) GenerateSitemapIndex() error {
	index := &SitemapIndex{
		Xmlns:    SITEMAP_NAMESPACE,
		Sitemaps: []SitemapEntry{},
	}

	for _, info := range sm.DB.Sitemaps {
		entry := SitemapEntry{
			Loc:     fmt.Sprintf("%s/%s", strings.TrimSuffix(sm.Config.BaseURL, "/"), info.Filename),
			LastMod: info.LastMod.Format("2006-01-02"),
		}
		index.Sitemaps = append(index.Sitemaps, entry)
	}

	output, err := xml.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	xmlData := []byte(xml.Header + string(output))
	indexPath := filepath.Join(sm.Config.OutputDir, "sitemap_index.xml")

	tempFile := indexPath + ".tmp"
	if err := os.WriteFile(tempFile, xmlData, 0644); err != nil {
		return err
	}

	return os.Rename(tempFile, indexPath)
}

func (sm *SitemapManager) PingSearchEngines() {
	sitemapURL := fmt.Sprintf("%s/sitemap_index.xml", strings.TrimSuffix(sm.Config.BaseURL, "/"))

	for _, engine := range sm.Config.PingEngines {
		pingURL := engine + sitemapURL
		resp, err := http.Get(pingURL)
		if err != nil {
			fmt.Printf("Failed to ping %s: %v\n", engine, err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("✓ Pinged %s successfully\n", engine)
	}
}

func (sm *SitemapManager) CheckForUpdates() (string, error) {
	if !sm.Config.CheckUpdates {
		return "", nil
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GITHUB_REPO)
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		return "", err
	}

	if release.TagName != "" && release.TagName != VERSION && release.TagName != "v"+VERSION {
		return release.TagName, nil
	}

	return "", nil
}

func (sm *SitemapManager) DownloadAndUpdate() error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GITHUB_REPO)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		return fmt.Errorf("failed to parse release info: %w", err)
	}

	if release.TagName == VERSION || release.TagName == "v"+VERSION {
		fmt.Println("✓ Already running the latest version")
		return nil
	}

	// Determine correct binary name for current platform
	var binaryName string
	switch runtime.GOOS {
	case "linux":
		binaryName = fmt.Sprintf("smx-%s-%s", runtime.GOOS, runtime.GOARCH)
	case "darwin":
		binaryName = fmt.Sprintf("smx-%s-%s", runtime.GOOS, runtime.GOARCH)
	case "windows":
		binaryName = fmt.Sprintf("smx-%s-%s.exe", runtime.GOOS, runtime.GOARCH)
	default:
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Find the correct asset
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("Downloading %s...\n", release.TagName)

	// Download new binary
	resp, err = client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Create temp file
	tempFile, err := os.CreateTemp("", "smx-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	// Write downloaded binary to temp file
	_, err = io.Copy(tempFile, resp.Body)
	tempFile.Close()
	if err != nil {
		return fmt.Errorf("failed to write update: %w", err)
	}

	// Make executable (Unix-like systems)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tempPath, 0755); err != nil {
			return fmt.Errorf("failed to make executable: %w", err)
		}
	}

	// Backup current binary
	backupPath := execPath + ".backup"
	if err := os.Rename(execPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Move new binary to executable path
	if err := os.Rename(tempPath, execPath); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, execPath)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	// Remove backup
	os.Remove(backupPath)

	fmt.Printf("✓ Successfully updated to %s\n", release.TagName)
	fmt.Println("Please restart smx to use the new version")

	return nil
}

// CLI Commands
var rootCmd = &cobra.Command{
	Use:   "smx",
	Short: "SitemapTool - Cross-platform sitemap manager",
	Long:  `A production-ready sitemap management tool with concurrent-safe operations`,
}

var addCmd = &cobra.Command{
	Use:   "add [url]",
	Short: "Add a URL to the sitemap",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := NewSitemapManager()
		if err != nil {
			return err
		}

		changefreq, _ := cmd.Flags().GetString("changefreq")
		priority, _ := cmd.Flags().GetFloat64("priority")

		if changefreq == "" {
			changefreq = sm.Config.DefaultChangefreq
		}
		if priority == 0 {
			priority = sm.Config.DefaultPriority
		}

		if err := sm.AddURL(args[0], changefreq, priority); err != nil {
			return err
		}

		fmt.Printf("✓ Added URL: %s\n", args[0])
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new sitemap",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := NewSitemapManager()
		if err != nil {
			return err
		}

		filename, err := sm.CreateNewSitemap()
		if err != nil {
			return err
		}

		if err := sm.SaveDB(); err != nil {
			return err
		}

		if err := sm.GenerateSitemapIndex(); err != nil {
			return err
		}

		fmt.Printf("✓ Created new sitemap: %s\n", filename)
		return nil
	},
}

var configCmd = &cobra.Command{
	Use:   "config [key] [value]",
	Short: "View or modify configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := NewSitemapManager()
		if err != nil {
			return err
		}

		if len(args) == 0 {
			data, _ := json.MarshalIndent(sm.Config, "", "  ")
			fmt.Println(string(data))
			fmt.Printf("\nConfig file: %s\n", sm.ConfigPath)
			return nil
		}

		if len(args) == 2 {
			key, value := args[0], args[1]

			configMap := make(map[string]interface{})
			data, _ := json.Marshal(sm.Config)
			json.Unmarshal(data, &configMap)

			if value == "true" || value == "false" {
				configMap[key] = (value == "true")
			} else if _, err := fmt.Sscanf(value, "%f", new(float64)); err == nil {
				var f float64
				fmt.Sscanf(value, "%f", &f)
				configMap[key] = f
			} else {
				configMap[key] = value
			}

			newData, _ := json.Marshal(configMap)
			json.Unmarshal(newData, sm.Config)

			if err := sm.SaveConfig(); err != nil {
				return err
			}

			fmt.Printf("✓ Updated %s = %s\n", key, value)
			return nil
		}

		return fmt.Errorf("usage: smx config [key] [value]")
	},
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show sitemap statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := NewSitemapManager()
		if err != nil {
			return err
		}

		fmt.Println("Sitemap Statistics")
		fmt.Println("==================")
		fmt.Printf("Total Sitemaps: %d\n", len(sm.DB.Sitemaps))
		fmt.Printf("Total URLs: %d\n", len(sm.DB.URLHashes))
		fmt.Printf("Output Directory: %s\n", sm.Config.OutputDir)
		fmt.Println("\nSitemaps:")

		for _, info := range sm.DB.Sitemaps {
			fmt.Printf("  - %s: %d URLs (last modified: %s)\n",
				info.Filename, info.URLCount, info.LastMod.Format("2006-01-02 15:04:05"))
		}

		return nil
	},
}

var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Ping search engines with sitemap",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := NewSitemapManager()
		if err != nil {
			return err
		}

		fmt.Println("Pinging search engines...")
		sm.PingSearchEngines()
		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("SitemapTool v%s\n", VERSION)
		fmt.Printf("OS: %s\n", runtime.GOOS)
		fmt.Printf("Arch: %s\n", runtime.GOARCH)
		fmt.Printf("Go Version: %s\n", runtime.Version())
		
		// Check for updates
		sm, err := NewSitemapManager()
		if err == nil && sm.Config.CheckUpdates {
			if newVersion, err := sm.CheckForUpdates(); err == nil && newVersion != "" {
				fmt.Printf("\n🔔 New version available: %s\n", newVersion)
				fmt.Println("Run 'smx update' to update automatically")
			}
		}
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update smx to the latest version",
	Long:  `Download and install the latest version of smx automatically`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := NewSitemapManager()
		if err != nil {
			return err
		}

		// Check if updates are disabled
		if !sm.Config.CheckUpdates {
			fmt.Println("Auto-updates are disabled in config")
			fmt.Println("Enable with: smx config check_updates true")
			return nil
		}

		// Check for updates first
		newVersion, err := sm.CheckForUpdates()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		if newVersion == "" {
			fmt.Println("✓ Already running the latest version")
			return nil
		}

		fmt.Printf("Current version: %s\n", VERSION)
		fmt.Printf("New version: %s\n", newVersion)
		fmt.Println()

		// Perform update
		return sm.DownloadAndUpdate()
	},
}

func init() {
	addCmd.Flags().String("changefreq", "", "Change frequency")
	addCmd.Flags().Float64("priority", 0, "Priority (0.0 to 1.0)")

	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(pingCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}