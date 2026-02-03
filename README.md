# Epson Proxy

A lightweight HTTP proxy server for Epson POS printers that accepts XML-based print commands and converts them to printer-specific byte sequences. Supports USB and TCP connections with automatic retry logic and graceful error handling.

## Supported Features

### Print Commands
- **Image Printing**: Base64-encoded monochrome images with automatic centering
- **Paper Cutting**: Full and partial cut commands
- **Cash Drawer**: Kick drawer/cash drawer pulse commands

### Connection Types
- **USB**: Direct USB device connection (e.g., `/dev/usb/lp0`) [UNSUPPORTED ON WINDOWS]
- **TCP**: Network-connected printers (e.g., `192.168.1.100:9100`)

### Protocol Features
- EPOS XML format parsing
- Automatic retry with connection recovery (configurable retry delay)
- HTTPS support with auto-generated self-signed certificates

## Unsupported Features

### Print Commands (Not Implemented)
- Text printing
- Paper feeding
- Any thing outside of print + cut + drawer

## Installation

### From Source
```bash
go build -o epson-proxy
```

### Pre-built Binaries
Download from the [Releases](https://github.com/thearyadev/epson-proxy/releases) page.

## Basic Setup Guide

### 1. USB Connection (Linux)
```bash
# Find your printer device
ls /dev/usb/lp*

# Run with USB connection
./epson-proxy -printer /dev/usb/lp0 -proto USB
```

### 2. TCP Connection (Network Printer)
```bash
./epson-proxy -printer 192.168.1.100:9100 -proto TCP
```

### 3. HTTPS Mode
```bash
./epson-proxy -printer /dev/usb/lp0 -proto USB -secure
```

## CLI Help Output

```
Usage: epson-proxy [options]

Options:
  -printer string
        Printer connection string (required)
        USB: /dev/usb/lp0
        TCP: 192.168.1.100:9100
  
  -proto string
        Protocol: USB or TCP (required)
  
  -receipt-width int
        Receipt width in pixels (default 576)
  
  -host string
        Server host (default "127.0.0.1")
  
  -port string
        Server port (default "8000")
  
  -secure
        Use HTTPS (auto-generates self-signed certificate)
```

## Example Usage

### Print an Image
Send an XML request to the proxy:

```bash
curl -X POST http://localhost:8000 \
  -H "Content-Type: application/xml" \
  -d '<?xml version="1.0" encoding="utf-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2011/03/epos-print">
  <image width="384" height="100">
    <base64-encoded-monochrome-image-data>
  </image>
  <cut/>
</epos-print>'
```

### Kick Cash Drawer
```bash
curl -X POST http://localhost:8000 \
  -H "Content-Type: application/xml" \
  -d '<?xml version="1.0" encoding="utf-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2011/03/epos-print">
  <pulse/>
</epos-print>'
```

## XML Format

The proxy accepts Epson's EPOS XML format:

```xml
<?xml version="1.0" encoding="utf-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2011/03/epos-print">
  <image width="384" height="100">
    iVBORw0KGgoAAAANSUhEUgAA... (base64 encoded monochrome image)
  </image>
  <pulse/>  <!-- Kick drawer -->
  <cut/>    <!-- Cut paper -->
</epos-print>
```

## License

MIT License - See [LICENSE](LICENSE)
