# Epson Proxy

A lightweight HTTP proxy server for Epson POS printers that accepts XML-based print commands and converts them to ESC/POS byte sequences. Supports USB device paths, TCP connections, and Windows RAW printer queues.

## Supported Features

### Print Commands
- **Image Printing**: Base64-encoded monochrome images with automatic centering
- **Paper Cutting**: Full and partial cut commands
- **Cash Drawer**: Kick drawer/cash drawer pulse commands

Images print consecutively without added blank feed lines. `<cut/>` uses Epson's [feed-and-cut command](https://download4.epson.biz/sec_pubs/pos/reference_en/escpos/gs_cv.html) (`GS V 65 0`) to advance to the cutter with no extra margin; the printer's physical print-head-to-cutter gap still applies.

### Connection Types
- **USB**: Direct USB device connection (e.g., `/dev/usb/lp0`) [UNSUPPORTED ON WINDOWS]
- **TCP**: Network-connected printers (e.g., `192.168.1.100:9100`)
- **RAW (Windows)**: Send ESC/POS bytes through the Windows print spooler to an installed printer queue (e.g., `Receipt RAW`), including USB printers

### Protocol Features
- EPOS XML format parsing
- Automatic retry with connection recovery for USB and TCP
- One Windows RAW spooler job per XML request, with no automatic retries after submission errors
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

Every branch push also builds snapshot archives for Linux, macOS, and Windows (amd64 and arm64). Open the matching [goreleaser Actions run](https://github.com/thearyadev/epson-proxy/actions/workflows/release.yml) and download the `epson-proxy-binaries` artifact. Windows ZIP archives contain `epson-proxy.exe`; Linux and macOS archives contain `epson-proxy`. The artifact also includes SHA-256 checksums. Tagged pushes continue publishing releases.

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

### 4. Windows RAW Printing (USB or Installed Queue)

Install your ESC/POS printer in Windows first. The queue must support RAW printer-ready data; XPS-only queues are not supported. The proxy uses the built-in Windows print spooler API, with no external Go dependencies or vendor SDK. See Microsoft's [RAW printing API documentation](https://learn.microsoft.com/en-us/windows/win32/printdocs/writeprinter).

Find the exact **queue name** in PowerShell:

```powershell
Get-Printer | Format-Table Name, DriverName, PortName
```

Use the value from `Name` for `-printer`, including spaces. For a queue named `Receipt RAW` on port `USB001`, pass `"Receipt RAW"`, not `USB001` or a Linux device path.

Download and extract the Windows binary from Releases, or build from source on Windows:

```powershell
go build -o epson-proxy.exe .
```

Start the proxy:

```powershell
.\epson-proxy.exe -proto RAW -printer "Receipt RAW" -receipt-width 384

# Optional document title in the Windows print queue, HTTPS, and CORS whitelist
.\epson-proxy.exe -proto RAW -printer "Receipt RAW" -receipt-width 384 -job-name "POS receipt" -secure -allow-origins "https://pos.example.com"

# Or run directly from source
go run . -proto RAW -printer "Receipt RAW" -receipt-width 384
```

Choose `-receipt-width` to match the printer's printable width in dots (for example, 384 or 576; default 576). This controls image centering and does not resize images. `-proto` accepts `USB`, `TCP`, and `RAW` case-insensitively. `RAW` is only available on Windows; selecting it elsewhere reports an error immediately. Windows network printers can also use `-proto TCP -printer "192.168.1.100:9100"`.

To test the HTTP server started by the first command above, run this in another PowerShell window. It prints a small black rectangle and cuts the paper:

```powershell
# 384 dots wide x 8 rows, packed as 48 bytes per row (1 = black).
$pixels = [byte[]](0..383 | ForEach-Object { 0xFF })
$imageData = [Convert]::ToBase64String($pixels)
$xml = @"
<epos-print xmlns="http://www.epson-pos.com/schemas/2011/03/epos-print">
  <image width="384" height="8">$imageData</image>
  <cut/>
</epos-print>
"@
Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:8000" -Method Post -ContentType "application/xml" -Body $xml
```

Clients continue sending EPOS XML over HTTP. `RAW` selects the printer transport; it does not make the HTTP endpoint accept arbitrary binary files, PNGs, or JPEGs. Image payloads must contain base64-encoded, row-major 1-bit raster bytes, with the most significant bit first and each row padded to `ceil(width / 8)` bytes.

Each request's images, drawer pulses, cuts, and final reset are submitted together. A successful response means the spooler accepted the job, not that paper output is confirmed. The log includes the spooler job ID. Failed RAW operations are not automatically retried because some output may already have printed; inspect the queue and printer before manually resubmitting. Startup errors report the queue or spooler failure immediately; check the queue name, the Windows Print Spooler service, and the current user's printing permissions.

## CLI Help Output

```
Usage: epson-proxy [options]

Options:
  -printer string
        Printer device path, TCP address, or Windows printer queue name (required)
        USB: /dev/usb/lp0
        TCP: 192.168.1.100:9100
        RAW: Receipt RAW (Windows queue name)
  
  -proto string
        Protocol: USB, TCP, or RAW (Windows only) (required)

  -job-name string
        Document title for Windows RAW print jobs (default "Epson Proxy")
  
  -receipt-width int
        Receipt width in pixels (default 576)
  
  -host string
        Server host (default "127.0.0.1")
  
  -port string
        Server port (default "8000")
  
  -secure
        Use HTTPS (auto-generates self-signed certificate)
  
  -allow-origins string
        Comma-separated list of allowed CORS origins (empty = allow all)
        Example: "https://example.com,https://app.example.com"

  -version
        Print version and exit
```

## CORS Configuration

By default, the server allows all origins (`*`). For production use, you should whitelist specific origins:

### Allow Specific Origins
```bash
./epson-proxy -printer /dev/usb/lp0 -proto USB -allow-origins "https://example.com,https://app.example.com"
```

### Blocked Origins
When CORS whitelist is configured, requests from non-whitelisted origins will receive a `403 Forbidden` response with the message: `CORS Error: Origin 'X' is not allowed`

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
    ... (base64-encoded packed 1-bit raster bytes, not a PNG/JPEG file)
  </image>
  <pulse/>  <!-- Kick drawer -->
  <cut/>    <!-- Cut paper -->
</epos-print>
```

## Security Considerations

### CORS Whitelisting
- Always configure `-allow-origins` in production environments
- The default behavior (allow all) is suitable for development only
- Origins are matched case-insensitively

### HTTPS
- Use `-secure` flag for encrypted connections
- Self-signed certificates are auto-generated for localhost
- For production, consider using proper SSL certificates

## License

MIT License - See [LICENSE](LICENSE)
