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
	pnk := try(func() {
		// Read the directory:
		fis = getPics()
	})

	var rsperr error
	if pnk != nil {
		var ok bool
		if rsperr, ok = pnk.(error); !ok {
			// Format the panic as a string if it's not an `error`:
			rsperr = fmt.Errorf("%v", pnk)
		}
		msg := rsperr.Error()
		log.Printf("ERROR: %s\n", msg)
		http.Error(rsp, msg, http.StatusInternalServerError)
		return
	}

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
		http.Error(rsp, "Method requires POST", http.StatusMethodNotAllowed)
		return
	}

	pnk := try(func() {
		reader, err := req.MultipartReader()
		if err != nil {
			panic(err)
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
			log.Printf("Accepting uploading: '%s'\n", destPath)

			f, err := os.Create(destPath)
			if err != nil {
				panic(fmt.Errorf("Could not create local file '%s'; error: %s", destPath, err.Error()))
			}
			defer f.Close()

			if _, err := io.Copy(f, part); err != nil {
				panic(err)
			}
		}
	})

	// Handle the panic:
	if pnk != nil {
		var msg string
		if err, ok := pnk.(error); ok {
			msg = err.Error()
		} else {
			msg = fmt.Sprint("%v", pnk)
		}

		log.Printf("ERROR: %s", msg)
		http.Error(rsp, "500 Internal Server Error", http.StatusInternalServerError)
		return
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
		panic(fmt.Errorf("Method requires POST"))
	}

	// Parse form data:
	if err := req.ParseForm(); err != nil {
		panic(err)
	}
	filename := req.Form.Get("filename")
	if filename == "" {
		panic(fmt.Errorf("Expecting filename form value"))
	}

	// Remove the file:
	destPath := path.Join(picsDir, path.Base(filename))
	if err := os.Remove(destPath); err != nil {
		panic(err)
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
		http.Error(rsp, "400 Bad Request - mime type of thumbnail requested is not image/jpeg", http.StatusBadRequest)
		return
	}

	// Locate the pic and the thumbnail:
	picPath := path.Join(picsDir, filename)
	thumbPath := path.Join(thumbsDir, filename)

	// Check if the pic file exists:
	picFI, err := os.Stat(picPath)
	if err != nil {
		http.Error(rsp, "404 Not Found - could not find original image to make thumbnail of", http.StatusNotFound)
		return
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
			http.Error(rsp, "404 Not Found - could not open original image to make thumbnail of", http.StatusNotFound)
			return
		}
		// Decode the JPEG:
		img, err := jpeg.Decode(pf)
		if err != nil {
			http.Error(rsp, "400 Bad Request - image is not a proper JPEG", http.StatusBadRequest)
			return
		}

		// Create the thumbnail file:
		tf, err := os.Create(thumbPath)
		defer tf.Close()
		if err != nil {
			http.Error(rsp, "500 Internal Server Error - could not create thumbnail file", http.StatusNotFound)
			return
		}

		//size := img.Bounds().Size()
		//if size.X

		thumbImg := resize.Resize(img, img.Bounds(), 64, 64)
		err = jpeg.Encode(tf, thumbImg, &jpeg.Options{Quality: 90})
		if err != nil {
			http.Error(rsp, "500 Internal Server Error - error while encoding JPEG", http.StatusInternalServerError)
			return
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
	mux.HandleFunc(rootURL, indexHandler)

	// Upload handler:
	uploadURL = pjoin(proxyRoot, "/upload")
	mux.HandleFunc(uploadURL, uploadHandler)

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
	mux.HandleFunc(thumbsURL, thumbHandler)

	// Start the HTTP server on the listening socket:
	log.Fatal(http.Serve(l, http.Handler(mux)))
}
