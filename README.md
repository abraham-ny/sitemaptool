# SitemapTool (smx)

A production-ready, cross-platform sitemap management tool with concurrent-safe operations.

## Features

- ✅ Cross-platform (Windows, Linux, macOS, Server-OSes)
- ✅ Concurrent-safe operations with file locking
- ✅ Automatic sitemap rotation at 50k URLs
- ✅ Sitemap index generation
- ✅ robots.txt compliance
- ✅ VCS awareness (.gitignore)
- ✅ Search engine ping support
- ✅ Auto-update checking
- ✅ Duplicate URL prevention

## Installation

### Option 1: Download Pre-built Binary
Download from [Releases](https://github.com/abraham-ny/sitemaptool/releases/latest)

### Option 2: Build from Source

```bash
# Clone repository
git clone https://github.com/abraham-ny/sitemaptool.git
cd sitemaptool

# Initialize Go module
make init

# Download dependencies
make deps

# Build
make build

# Install globally
make install
```

## Development Setup

### Prerequisites
- Go 1.21 or higher
- Make (optional, for convenience)

### Quick Start

```bash
# 1. Clone the repository
git clone https://github.com/abraham-ny/sitemaptool.git
cd sitemaptool

# 2. Initialize Go module
go mod init github.com/abraham-ny/sitemaptool

# 3. Install dependencies
go get github.com/spf13/cobra@v1.8.0
go mod tidy

# 4. Build
go build -o smx main.go

# 5. Run
./smx help
```

### Using Makefile (Recommended)

```bash
make init    # Initialize module
make deps    # Download dependencies
make build   # Build for current platform
make test    # Run tests
make install # Install globally
```

## Usage

### Initialize Configuration
```bash
smx config
```

### Configure Base URL
```bash
smx config base_url https://yoursite.com
smx config output_dir ./public/sitemaps
smx config ping_on_update true
```

### Add URLs
```bash
# Basic
smx add https://yoursite.com/page1

# With options
smx add https://yoursite.com/page2 --changefreq daily --priority 0.8
```

### Create New Sitemap
```bash
smx create
```

### View Statistics
```bash
smx stats
```

### Ping Search Engines
```bash
smx ping
```

### Check Version
```bash
smx version
```

## Configuration

Config file location: `~/.sitemaptool/config.json`

```json
{
  "output_dir": "./sitemaps",
  "base_url": "https://example.com",
  "sitemap_prefix": "sitemap",
  "ping_on_update": false,
  "ping_engines": [
    "https://www.google.com/ping?sitemap=",
    "https://www.bing.com/ping?sitemap="
  ],
  "default_changefreq": "weekly",
  "default_priority": 0.5,
  "respect_robots": true,
  "vcs_aware": true,
  "robots_path": "./robots.txt",
  "check_updates": true
}
```

## Building Releases

### Manual Build
```bash
make build-all
```

### GitHub Actions
Push a tag to trigger automated builds:
```bash
git tag v1.0.0
git push origin v1.0.0
```

## Integration Examples

### CI/CD Pipeline
```yaml
# .github/workflows/update-sitemap.yml
name: Update Sitemap

on:
  push:
    branches: [main]

jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Download smx
        run: |
          wget https://github.com/abraham-ny/sitemaptool/releases/latest/download/smx-linux-amd64
          chmod +x smx-linux-amd64
          sudo mv smx-linux-amd64 /usr/local/bin/smx
      
      - name: Update sitemap
        run: |
          smx config base_url https://yoursite.com
          smx add https://yoursite.com/new-page
```

### Cron Job
```bash
# Add to crontab
0 2 * * * cd /path/to/site && /usr/local/bin/smx add https://yoursite.com/daily-page
```

### Web Hook
```go
http.HandleFunc("/webhook/new-content", func(w http.ResponseWriter, r *http.Request) {
    url := r.FormValue("url")
    cmd := exec.Command("smx", "add", url)
    cmd.Run()
})
```

## License

MIT License

## Contributing

Pull requests welcome! Please ensure tests pass and code is formatted.

