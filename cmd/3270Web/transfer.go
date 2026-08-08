package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/host"
)

// IND$FILE file transfer.
//
// Getting a dataset off the host into a spreadsheet, or a file up to it, is
// routine work in a 3270 shop that otherwise means leaving 3270Web entirely.
//
// The security shape of this feature is worth stating plainly, because it is
// the difference between a file transfer and an arbitrary file read/write on
// the server: **the local path is always chosen by the server**. A request
// says which HOST file to move and in which direction; it never says where
// on the server disk to put it. Uploads land in a per-transfer temporary
// directory that is removed when the request ends, and downloads are streamed
// from one and then deleted.

const (
	// maxTransferUploadBytes caps an upload. IND$FILE is not a bulk transport
	// — it is a 3270 data stream — and multi-megabyte transfers over it are
	// slow enough to be a mistake rather than an intention.
	maxTransferUploadBytes = 16 << 20 // 16 MiB
	// maxTransferDownloadBytes caps what will be streamed back, so a runaway
	// host-side file cannot exhaust the server's disk or the browser.
	maxTransferDownloadBytes = 64 << 20 // 64 MiB
)

// transferCapable narrows the session's host to the transfer capability. The
// mock host used by tests and the built-in sample apps does not implement it,
// so a session on one of those gets a clear message rather than a panic.
type transferCapable interface {
	Transfer(opts host.TransferOptions) (string, error)
}

// transferOptionsFromRequest reads the shared option fields. LocalFile and
// Direction are deliberately NOT read from the request.
func transferOptionsFromRequest(c *gin.Context) host.TransferOptions {
	atoi := func(name string) int {
		n, err := strconv.Atoi(strings.TrimSpace(c.PostForm(name)))
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	return host.TransferOptions{
		HostFile:       strings.TrimSpace(c.PostForm("hostFile")),
		HostType:       strings.TrimSpace(c.PostForm("hostType")),
		Mode:           strings.TrimSpace(c.PostForm("mode")),
		Cr:             strings.TrimSpace(c.PostForm("cr")),
		Exist:          strings.TrimSpace(c.PostForm("exist")),
		Recfm:          strings.TrimSpace(c.PostForm("recfm")),
		Lrecl:          atoi("lrecl"),
		Blksize:        atoi("blksize"),
		PrimarySpace:   atoi("primarySpace"),
		SecondarySpace: atoi("secondarySpace"),
		Allocation:     strings.TrimSpace(c.PostForm("allocation")),
	}
}

// transferrer returns the active session's host if it can do transfers.
func (app *App) transferrer(c *gin.Context) (transferCapable, bool) {
	s := app.getSession(c)
	if s == nil {
		return nil, false
	}
	h := app.sessionHost(s)
	if h == nil {
		return nil, false
	}
	t, ok := h.(transferCapable)
	return t, ok
}

// TransferSendHandler uploads a file to the host with IND$FILE.
func (app *App) TransferSendHandler(c *gin.Context) {
	t, ok := app.transferrer(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File transfer needs a live TN3270 session (the built-in sample apps do not support it).",
		})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTransferUploadBytes)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choose a file to send"})
		return
	}
	if fileHeader.Size > maxTransferUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("file is larger than the %d MiB transfer limit", maxTransferUploadBytes>>20),
		})
		return
	}

	// The server picks the path. Nothing from the request reaches the
	// filesystem as a path — only as file *contents*.
	dir, err := os.MkdirTemp("", "3270web-send-")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not stage the upload"})
		return
	}
	defer os.RemoveAll(dir)
	localPath := filepath.Join(dir, "upload.bin")

	if err := c.SaveUploadedFile(fileHeader, localPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not stage the upload"})
		return
	}

	opts := transferOptionsFromRequest(c)
	opts.Direction = host.TransferSend
	opts.LocalFile = localPath

	message, err := t.Transfer(opts)
	// Both outcomes are recorded: moving data onto a mainframe is the kind of
	// act somebody asks about afterwards, and an attempt that failed is still
	// an attempt. The host-side name is the target; the contents are not.
	app.auditTransfer(c, host.TransferSend, opts.HostFile, err)
	if err != nil {
		log.Printf("IND$FILE send failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"bytes":   fileHeader.Size,
		"message": message,
	})
}

// TransferReceiveHandler downloads a host file with IND$FILE and streams it
// to the browser.
func (app *App) TransferReceiveHandler(c *gin.Context) {
	t, ok := app.transferrer(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File transfer needs a live TN3270 session (the built-in sample apps do not support it).",
		})
		return
	}

	opts := transferOptionsFromRequest(c)
	opts.Direction = host.TransferReceive
	// Receiving into an existing file is meaningless here — the destination
	// is a fresh temp file every time — so the host-side option is fixed.
	opts.Exist = "replace"

	dir, err := os.MkdirTemp("", "3270web-recv-")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not stage the download"})
		return
	}
	defer os.RemoveAll(dir)
	localPath := filepath.Join(dir, "download.bin")
	opts.LocalFile = localPath

	_, err = t.Transfer(opts)
	app.auditTransfer(c, host.TransferReceive, opts.HostFile, err)
	if err != nil {
		log.Printf("IND$FILE receive failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	info, err := os.Stat(localPath)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "the transfer reported success but produced no file"})
		return
	}
	if info.Size() > maxTransferDownloadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("received file is larger than the %d MiB limit", maxTransferDownloadBytes>>20),
		})
		return
	}

	file, err := os.Open(localPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the received file"})
		return
	}
	defer file.Close()

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))
	c.Header("Content-Disposition", contentDisposition(downloadName(opts.HostFile)))
	if _, err := io.Copy(c.Writer, file); err != nil {
		log.Printf("IND$FILE receive: streaming to client failed: %v", err)
	}
}

// downloadName turns a host dataset name into something a desktop will
// accept, e.g. "USER.DATA(MEMBER)" -> "USER.DATA.MEMBER".
func downloadName(hostFile string) string {
	name := strings.TrimSpace(hostFile)
	name = strings.NewReplacer("(", ".", ")", "", "/", "-", "\\", "-").Replace(name)
	name = strings.Trim(name, ". ")
	if name == "" {
		return "download.txt"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// contentDisposition builds the header with a quoted ASCII fallback, keeping
// any character that could break out of the header out of it.
func contentDisposition(name string) string {
	safe := strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r == 127 {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf("attachment; filename=%q", safe)
}

// auditTransfer records a file moving between the browser and the mainframe.
//
// The host-side dataset name goes in because that is what the act was about.
// The bytes never do: this is a trail of what happened, not a copy of what
// was moved.
func (app *App) auditTransfer(c *gin.Context, direction host.TransferDirection, hostFile string, err error) {
	outcome := audit.Success
	detail := map[string]string{"direction": string(direction)}
	if err != nil {
		outcome = audit.Failure
	}
	app.auditRequest(c, audit.EventFileTransfer, outcome, strings.TrimSpace(hostFile), detail)
}
