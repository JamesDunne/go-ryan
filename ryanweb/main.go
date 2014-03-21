package main

import (
	"flag"
	"fmt"
	"html/template"
	//"image"
	"image/jpeg"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

import (
	"github.com/JamesDunne/go-ryan/resize"
)

// Web host info:
var proxyRoot, siteHost string

// Parsed HTML templates:
var templates *template.Template

// Configured URLs based on commandline arguments:
var rootURL, picsURL, thumbsURL, deleteURL, uploadURL, listURL string
var picsDir, thumbsDir string

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		panic(err)
	}
	return abs
}

// Reads the /pics/ directory:
func getPics() []os.FileInfo {
	// Open the directory to read its contents:
	f, err := os.Open(picsDir)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// Read the directory entries:
	fis, err := f.Readdir(0)
	if err != nil {
		panic(err)
	}

	// Remove the opened files from the list (presume they are in mid-upload via SFTP):

	// Sort the entries by the desired mode:
	sort.Sort(ByDate{fis, sortDescending})

	return fis
}

func getMimeType(filename string) string {
	return mime.TypeByExtension(strings.ToLower(path.Ext(filename)))
}

type FileViewModel struct {
	Name     string
	Size     int64
	Mime     string
	LastMod  string
	PicURL   string
	ThumbURL string
}

type IndexViewModel struct {
	DeleteURL string
	UploadURL string
	Files     []FileViewModel
}

// HTML handler for `/`:
func indexHandler(rsp http.ResponseWriter, req *http.Request) {
	if req.URL.Path != rootURL {
		http.Error(rsp, "404 Not Found", http.StatusNotFound)
		return
	}

	var fis []os.FileInfo
	// Read the directory:
	fis = getPics()

	// Successful response:
	rsp.Header().Add("Content-Type", "text/html; charset=utf-8")
	rsp.WriteHeader(http.StatusOK)

	// Convert the os.FileInfos to a more HTML-friendly model:
	model := IndexViewModel{
		DeleteURL: deleteURL,
		UploadURL: uploadURL,
		Files:     make([]FileViewModel, 0, len(fis)),
	}
	for _, fi := range fis {
		model.Files = append(model.Files, FileViewModel{
			Name:     fi.Name(),
			Size:     fi.Size(),
			Mime:     getMimeType(fi.Name()),
			LastMod:  fi.ModTime().String(),
			PicURL:   pjoin(picsURL, fi.Name()),
			ThumbURL: pjoin(thumbsURL, fi.Name()),
		})
	}

	// Execute the HTML template:
	templates.ExecuteTemplate(rsp, "index.html", model)
}

// HTML handler for `/upload`:
func uploadHandler(rsp http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		panic(NewHttpError(http.StatusMethodNotAllowed, "Upload requires POST method", fmt.Errorf("Upload requires POST method")))
	}

	reader, err := req.MultipartReader()
	if err != nil {
		panic(NewHttpError(http.StatusBadRequest, "Error parsing multipart form data", err))
	}

	// Keep reading the multipart form data and handle file uploads:
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if part.FileName() == "" {
			continue
		}

		// Copy upload data to a local file:
		destPath := path.Join(picsDir, part.FileName())
		log.Printf("Accepting upload: '%s'\n", destPath)

		f, err := os.Create(destPath)
		if err != nil {
			panic(NewHttpError(http.StatusInternalServerError, "Could not accept upload", fmt.Errorf("Could not create local file '%s'; %s", destPath, err.Error())))
		}
		defer f.Close()

		if _, err := io.Copy(f, part); err != nil {
			panic(NewHttpError(http.StatusInternalServerError, "Could not write upload data to local file", fmt.Errorf("Could not write to local file '%s'; %s", destPath, err)))
		}
	}

	// 302 to `/`:
	http.Redirect(rsp, req, rootURL, http.StatusFound)
}

func extractNames(fis []os.FileInfo) []string {
	names := make([]string, len(fis), len(fis))
	for i := range fis {
		names[i] = fis[i].Name()
	}
	return names
}

// JSON handler for `/list.php`:
func listJsonHandler(req *http.Request) (result interface{}) {
	fis := getPics()

	return struct {
		BaseUrl string   `json:"baseUrl"`
		Files   []string `json:"files"`
	}{
		BaseUrl: pjoin(siteHost, picsURL),
		Files:   extractNames(fis),
	}
}

// JSON handler for `/delete`:
func deleteJsonHandler(req *http.Request) (result interface{}) {
	if req.Method != "POST" {
		panic(NewHttpError(http.StatusMethodNotAllowed, "Upload requires POST method", fmt.Errorf("Upload requires POST method")))
	}

	// Parse form data:
	if err := req.ParseForm(); err != nil {
		panic(NewHttpError(http.StatusBadRequest, "Error parsing form data", err))
	}
	filename := req.Form.Get("filename")
	if filename == "" {
		panic(NewHttpError(http.StatusBadRequest, "Expecting filename form value", fmt.Errorf("No filename POST value")))
	}

	// Remove the file:
	destPath := path.Join(picsDir, path.Base(filename))
	if err := os.Remove(destPath); err != nil {
		panic(NewHttpError(http.StatusBadRequest, "Unable to delete file", fmt.Errorf("Unable to delete file '%s': %s", destPath, err)))
	}

	return struct {
		Success bool `json:"success"`
	}{
		Success: true,
	}
}

// File server for `/thumbs/*`:
func thumbHandler(rsp http.ResponseWriter, req *http.Request) {
	filename := removePrefix(req.URL.Path, thumbsURL)

	mimeType := getMimeType(filename)
	if mimeType != "image/jpeg" {
		panic(NewHttpError(http.StatusBadRequest, "mime type of thumbnail requested is not image/jpeg", fmt.Errorf("mime type of '%s' is '%s'", filename, mimeType)))
	}

	// Locate the pic and the thumbnail:
	picPath := path.Join(picsDir, filename)
	thumbPath := path.Join(thumbsDir, filename)

	// Check if the pic file exists:
	picFI, err := os.Stat(picPath)
	if err != nil {
		panic(NewHttpError(http.StatusBadRequest, "could not find original image to make thumbnail of", fmt.Errorf("cannot find image at '%s'", picPath)))
	}

	// Check if the thumbnail file exists:
	thumbFI, err := os.Stat(thumbPath)
	if err == nil {
		// If the modtime on the thumbnail is after the pic, serve the thumbnail file:
		if thumbFI.ModTime().After(picFI.ModTime()) {
			http.ServeFile(rsp, req, thumbPath)
			return
		}
	}

	// Create a new thumbnail:
	{
		// Open the original image:
		pf, err := os.Open(picPath)
		defer pf.Close()
		if err != nil {
			panic(NewHttpError(http.StatusNotFound, "could not open original image to make thumbnail of", fmt.Errorf("cannot open image file at '%s'", picPath)))
		}
		// Decode the JPEG:
		img, err := jpeg.Decode(pf)
		if err != nil {
			panic(NewHttpError(http.StatusBadRequest, "image is not a proper JPEG", fmt.Errorf("image file is not a JPEG: '%s'", picPath)))
		}

		// Create the thumbnail file:
		tf, err := os.Create(thumbPath)
		defer tf.Close()
		if err != nil {
			panic(NewHttpError(http.StatusInternalServerError, "could not create thumbnail file", fmt.Errorf("could not create thumbnail file at '%s'; %s", thumbPath, err)))
		}

		// TODO: calculate the largest square bounds for a thumbnail to preserve aspect ratio

		thumbImg := resize.Resize(img, img.Bounds(), 64, 64)
		err = jpeg.Encode(tf, thumbImg, &jpeg.Options{Quality: 90})
		if err != nil {
			panic(NewHttpError(http.StatusInternalServerError, "error while encoding JPEG", fmt.Errorf("failed encoding JPEG for '%s': %s", thumbPath, err)))
		}
	}

	// Serve the thumbnail:
	http.ServeFile(rsp, req, thumbPath)
	return
}

func main() {
	var socketType string
	var socketAddr string
	var templatesDir string

	// TODO(jsd): Make this pair of arguments a little more elegant, like "unix:/path/to/socket" or "tcp://:8080"
	flag.StringVar(&socketType, "l", "tcp", `type of socket to listen on; "unix" or "tcp" (default)`)
	flag.StringVar(&socketAddr, "a", ":8080", `address to listen on; ":8080" (default TCP port) or "/path/to/unix/socket"`)

	flag.StringVar(&siteHost, "host", "http://ryan.bittwiddlers.org", "site host (scheme://host:port)")
	flag.StringVar(&proxyRoot, "p", "/", "root of web requests to process")
	flag.StringVar(&templatesDir, "tmpl", "./tmpl", "local filesystem path to HTML templates")
	flag.StringVar(&picsDir, "pics", "./pics", "local filesystem path to store pictures")
	flag.StringVar(&thumbsDir, "thumbs", "./thumbs", "local filesystem path to cache thumbnails")
	flag.Parse()

	// Clean up args:
	siteHost = removeSuffix(siteHost, "/")
	proxyRoot = removeSuffix(proxyRoot, "/")

	templatesDir = removeSuffix(templatesDir, "/")
	picsDir = removeSuffix(picsDir, "/")
	thumbsDir = removeSuffix(thumbsDir, "/")

	// Canonicalize local paths (follow symlinks):
	templatesDir = canonicalPath(templatesDir)
	picsDir = canonicalPath(picsDir)
	thumbsDir = canonicalPath(thumbsDir)

	log.Printf("templates: %s\n", templatesDir)
	log.Printf("pics:      %s\n", picsDir)
	log.Printf("thumbs:    %s\n", thumbsDir)

	// Create directories if they don't exist:
	if _, err := os.Stat(picsDir); err != nil {
		os.Mkdir(picsDir, 0775)
	}
	if _, err := os.Stat(thumbsDir); err != nil {
		os.Mkdir(thumbsDir, 0775)
	}

	// Parse HTML templates:
	templates = template.Must(template.ParseGlob(path.Join(templatesDir, "*.html")))

	// Create the socket to listen on:
	l, err := net.Listen(socketType, socketAddr)
	if err != nil {
		log.Fatal(err)
		return
	}

	// NOTE(jsd): Unix sockets must be unlink()ed before being reused again.

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for a signal:
		sig := <-c
		log.Printf("Caught signal '%s': shutting down.", sig)
		// Stop listening:
		l.Close()
		// Delete the unix socket, if applicable:
		if socketType == "unix" {
			os.Remove(socketAddr)
		}
		// And we're done:
		os.Exit(0)
	}(sigc)

	// Set up the request multiplexer:
	mux := http.NewServeMux()
	rootURL = pjoin(proxyRoot, "/")
	mux.Handle(rootURL, NewErrorHandler(indexHandler))

	// Upload handler:
	uploadURL = pjoin(proxyRoot, "/upload")
	mux.Handle(uploadURL, NewErrorHandler(uploadHandler))

	// JSON list handler:
	listURL = pjoin(proxyRoot, "/list")
	mux.Handle(listURL, NewJsonHandler(listJsonHandler))

	// Delete handler:
	deleteURL = pjoin(proxyRoot, "/delete")
	mux.Handle(deleteURL, NewJsonHandler(deleteJsonHandler))

	// Serve /pics/ from the folder:
	picsURL = pjoin(proxyRoot, "/pics/")
	mux.Handle(picsURL, http.StripPrefix(picsURL, http.FileServer(http.Dir(picsDir))))

	// Serve /thumbs/ requests dynamically with a filesystem-backed cache:
	thumbsURL = pjoin(proxyRoot, "/thumbs/")
	mux.Handle(thumbsURL, NewErrorHandler(thumbHandler))

	// Start the HTTP server on the listening socket:
	log.Fatal(http.Serve(l, http.Handler(mux)))
}
