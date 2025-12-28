# Picture Metadata Organizer

A Go-based tool for reorganizing and standardizing photo collections with automatic metadata updates.

## Features

- **Intelligent Date Parsing**: Extracts dates from various filename formats (YYYY-MM-DD, YYYYMMDD, YYMMDD, etc.)
- **Standardized Organization**: Reorganizes photos into `YYYY/YYYY-MM/` directory structure
- **Filename Standardization**: Renames files to `YYYY-MM-DD_HHMMSS_description.ext` format
- **EXIF Metadata Updates**: Updates photo metadata (DateTimeOriginal, CreateDate) to match filename dates
- **SSH Support**: Can process photos directly from remote servers
- **Remote Destination**: Can write reorganized photos back to remote server (NAS to NAS processing)
- **Dry Run Mode**: Preview changes before applying them
- **Comprehensive Statistics**: Tracks processed, skipped, and error files

## Prerequisites

### Option 1: Docker (Recommended)
- **Docker** only - everything else is packaged in the container!

### Option 2: Native Installation
1. **Go 1.24+** (automatically downloaded if needed)
2. **exiftool** (for EXIF metadata updates)
   - Install: `sudo apt-get install libimage-exiftool-perl` (Debian/Ubuntu)
   - Or download from: https://exiftool.org/

## Installation

### Docker Installation (Recommended)

```bash
# Build the Docker image
docker build -t picture-metadata:latest .

# Or use docker-compose
docker-compose build
```

### Native Installation

```bash
# Build the binary
GOTOOLCHAIN=auto go build -o picture-metadata

# Optionally, install to PATH
sudo cp picture-metadata /usr/local/bin/
```

## Usage

### Using Docker (Recommended)

#### Quick Run with Wrapper Script

```bash
# Use the provided wrapper script
./run-docker.sh -source /path/to/source -dest /data/output -dry-run -verbose
```

#### Docker Run

```bash
# Dry run from remote SSH
docker run --rm \
  -v ~/.ssh:/root/.ssh:ro \
  -v $(pwd)/output:/data/output \
  picture-metadata:latest \
  -source "/var/services/homes/redgoat650/Photos/Jane Photos/Curated Photos" \
  -dest /data/output \
  -ssh-host nas-photos \
  -dry-run \
  -verbose
```

#### Docker Compose

```bash
# Edit docker-compose.yml to configure your paths
# Then run:
docker-compose up
```

### Native Usage

#### Basic Usage (Local Files)

```bash
./picture-metadata -source /path/to/source -dest /path/to/destination
```

#### Dry Run (Preview Changes)

```bash
./picture-metadata -source /path/to/source -dest /path/to/destination -dry-run
```

#### Remote SSH Source

```bash
./picture-metadata \
  -source "/var/services/homes/redgoat650/Photos/Jane Photos/Curated Photos" \
  -dest ./reorganized-photos \
  -ssh-host nas-photos \
  -verbose \
  -dry-run
```

#### Remote to Remote (NAS to NAS)

Process photos on NAS and write reorganized output back to NAS:

```bash
docker run --rm \
  -v ~/.ssh:/root/.ssh:ro \
  --add-host nas-photos:142.254.0.235 \
  picture-metadata:latest \
  -source "/var/services/homes/redgoat650/Photos/Jane Photos/Curated Photos" \
  -dest "/var/services/homes/redgoat650/Photos/Organized" \
  -ssh-host redgoat650@nas-photos:69 \
  -remote-dest \
  -verbose
```

This mode downloads each photo temporarily, updates EXIF metadata, then uploads to the destination path on the NAS.
### Command-Line Options

- `-source <path>`: Source directory containing photos (required)
- `-dest <path>`: Destination directory for reorganized photos (required)
- `-dry-run`: Preview changes without actually moving/modifying files
- `-ssh-host <host>`: SSH host for source (e.g., `nas-photos` or `user@host:port`)
- `-remote-dest`: Enable remote destination mode (writes back to NAS)
- `-dest-ssh-host <host>`: SSH host for destination (defaults to same as source)
- `-verbose`: Enable detailed logging

## How It Works

### 1. Date Extraction

The tool parses dates from filenames using these patterns:

- `YYYY_MM_DD_description.jpg` → 2024-03-15
- `YYYYMMDD_description.jpg` → 2024-03-15  
- `YYMMDD_description.jpg` → 2024-03-15 (assumes 19XX or 20XX)
- `YYYY_description.jpg` → 2024-01-01 (defaults to Jan 1)

### 2. Standardized Output Structure

**Directory Structure:**
```
destination/
├── 1954/
│   ├── 1954-01/
│   │   └── 1954-01-15_120000_Christmas_ourbeach.jpg
│   ├── 1954-12/
│       └── 1954-12-25_120000_house_front.jpg
├── 2024/
    ├── 2024-09/
        └── 2024-09-28_120000_England_(25).jpg
```

**Filename Format:** `YYYY-MM-DD_HHMMSS_description.ext`

### 3. Metadata Updates

For each photo, the tool updates:
- `DateTimeOriginal`
- `CreateDate`
- `ModifyDate`

All set to match the date extracted from the filename.

## Example Workflow

### Step 1: Set up SSH access (if using remote files)

```bash
# Generate SSH key
ssh-keygen -t ed25519 -f ~/.ssh/nas_key -N ""

# Copy to server
ssh-copy-id -p 69 -i ~/.ssh/nas_key.pub redgoat650@142.254.0.235

# Add to SSH config
cat >> ~/.ssh/config << 'EOF'
Host nas-photos
    HostName 142.254.0.235
    Port 69
    User redgoat650
    IdentityFile ~/.ssh/nas_key
EOF
```

### Step 2: Dry run to preview changes

```bash
./picture-metadata \
  -source "/var/services/homes/redgoat650/Photos/Jane Photos/Curated Photos" \
  -dest ./reorganized-photos \
  -ssh-host nas-photos \
  -dry-run \
  -verbose
```

### Step 3: Execute the reorganization

```bash
./picture-metadata \
  -source "/var/services/homes/redgoat650/Photos/Jane Photos/Curated Photos" \
  -dest ./reorganized-photos \
  -ssh-host nas-photos \
  -verbose
```

## Supported File Formats

- JPEG (`.jpg`, `.jpeg`)
- PNG (`.png`)
- GIF (`.gif`)
- BMP (`.bmp`)
- TIFF (`.tif`, `.tiff`)
- HEIC/HEIF (`.heic`, `.heif`)

## Statistics Output

After processing, you'll see:

```
=== Processing Statistics ===
Total files found:      1234
Successfully processed: 1200
Skipped (no date):      20
Errors:                 14
Files moved:            1200
Metadata updated:       1200
============================
```

## Notes

- The original files are **copied**, not moved (originals remain intact)
- Files without parseable dates in filenames are skipped
- Synology `@eaDir` metadata directories are automatically ignored
- If exiftool is not installed, files will still be reorganized but metadata won't be updated

## License

MIT License
