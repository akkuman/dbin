// TODO: be sure
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/goccy/go-json"
	minify "github.com/tdewolff/minify/v2"
	mjson "github.com/tdewolff/minify/v2/json"
)

type labeledString struct {
	mainURL           string
	fallbackURL       string
	resolveToFinalURL bool
}

type Item struct {
	RealName    string `json:"bin_name"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Note        string `json:"note,omitempty"`
	Version     string `json:"version,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	Size        string `json:"size,omitempty"`
	Bsum        string `json:"bsum,omitempty"`   // BLAKE3
	Shasum      string `json:"shasum,omitempty"` // SHA256
	BuildDate   string `json:"build_date,omitempty"`
	SrcURL      string `json:"src_url,omitempty"`
	WebURL      string `json:"web_url,omitempty"`
	BuildScript string `json:"build_script,omitempty"`
	BuildLog    string `json:"build_log,omitempty"`
	Category    string `json:"category,omitempty"`
	ExtraBins   string `json:"extra_bins,omitempty"`
	Icon        string `json:"icon"`
}

type Metadata struct {
	Bin  []Item `json:"bin"`
	Pkg  []Item `json:"pkg"`
	Base []Item `json:"base"`
}

var repoLabels = map[string]string{
	"bin":  "Toolpacks",
	"pkg":  "Toolpacks-extras",
	"base": "Baseutils",
}

func urldecode(encoded string) (string, error) {
	return url.PathUnescape(encoded)
}

func processItems(items []Item, realArchs, validatedArchs []string, repo labeledString, section string) []Item {
	for i, item := range items {
		// If resolveToFinalURL is false, skip URL transformation
		if !repo.resolveToFinalURL {
			continue
		}

		// Parse the download URL to get its path
		parsedURL, err := url.Parse(item.DownloadURL)
		if err != nil {
			continue
		}

		// Extract the path from the URL and remove leading slashes
		cleanPath := parsedURL.Path
		if strings.HasPrefix(cleanPath, "/") {
			cleanPath = cleanPath[1:]
		}

		// Remove the architecture-specific path from the download URL path
		for _, prefix := range append(realArchs, validatedArchs...) {
			if strings.HasPrefix(cleanPath, prefix) {
				cleanPath = strings.TrimPrefix(cleanPath, prefix+"/")
				break
			}
		}

		// Remove the repo's label based on the section
		if label, ok := repoLabels[section]; ok {
			if strings.HasPrefix(cleanPath, label+"/") {
				cleanPath = strings.TrimPrefix(cleanPath, label+"/")
			}
		}

		// Set the correct `RealName` field based on the path
		items[i].RealName = cleanPath
	}
	return items
}

func downloadJSON(url string) (Metadata, error) {
	resp, err := http.Get(url)
	if err != nil {
		return Metadata{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Metadata{}, err
	}

	var metadata Metadata
	err = json.Unmarshal(body, &metadata)
	if err != nil {
		return Metadata{}, err
	}

	return metadata, nil
}

func saveJSON(filename string, items []Item) error {
	// Marshal JSON with indentation
	jsonData, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	// Write the pretty-printed JSON to the file
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return err
	}

	// Minify and save the file
	return minifyJSON(filename, jsonData)
}

func minifyJSON(filename string, jsonData []byte) error {
	// Create a new minifier
	m := minify.New()
	m.AddFunc("application/json", mjson.Minify)

	// Minify the JSON data
	minifiedData, err := m.Bytes("application/json", jsonData)
	if err != nil {
		return err
	}

	// Create the minified file name
	minFilename := strings.TrimSuffix(filename, ".json") + ".min.json"

	// Write the minified data to a new file
	err = os.WriteFile(minFilename, minifiedData, 0644)
	if err != nil {
		return err
	}

	return nil
}

func downloadWithFallback(repo labeledString) (Metadata, error) {
	metadata, err := downloadJSON(repo.mainURL)
	if err == nil {
		fmt.Printf("Downloaded JSON from: %s\n", repo.mainURL)
		return metadata, nil
	}

	fmt.Printf("Error downloading from main URL %s: %v. Trying fallback URL...\n", repo.mainURL, err)
	metadata, err = downloadJSON(repo.fallbackURL)
	if err == nil {
		fmt.Printf("Downloaded JSON from fallback URL: %s\n", repo.fallbackURL)
		return metadata, nil
	}

	fmt.Printf("Error downloading from fallback URL %s: %v\n", repo.fallbackURL, err)
	return Metadata{}, err
}

func main() {
	validatedArchs := []string{"amd64_linux", "arm64_linux"}
	realArchs := []string{"x86_64_Linux", "x86_64-Linux", "aarch64_Linux", "aarch64-Linux", "aarch64_arm64_Linux", "aarch64_arm64-Linux", "x86_64", "x64_Windows"}
	//realArchs := []string{"x86_64", "x86_64_Linux", "aarch64_Linux", "aarch64_arm64_Linux"}

	// Loop over the indices to access both validatedArchs and realArchs
	for i := range validatedArchs {
		arch := validatedArchs[i]

		repos := []labeledString{
			{"https://bin.ajam.dev/" + arch + "/METADATA.AIO.min.json",
				"https://pkg.ajam.dev/" + arch + "/METADATA.AIO.min.json",
				true},
		}

		for _, repo := range repos {
			metadata, err := downloadWithFallback(repo)
			if err != nil {
				fmt.Printf("Error downloading JSON from both main and fallback URLs for repo: %v\n", err)
				continue
			}

			save := func(outputFile string, items []Item) {
				if err := saveJSON(outputFile, items); err != nil {
					fmt.Printf("Error saving JSON to %s: %v\n", outputFile, err)
					return
				}
				fmt.Printf("Processed and saved to %s\n", outputFile)
			}

			// Process bin items
			processedBinItems := processItems(metadata.Bin, realArchs, validatedArchs, repo, "bin")
			// Process pkg items
			processedPkgItems := processItems(metadata.Pkg, realArchs, validatedArchs, repo, "pkg")
			// Process base items
			processedBaseItems := processItems(metadata.Base, realArchs, validatedArchs, repo, "base")

			// Merge all items into a single slice
			allItems := append(append(processedBinItems, processedPkgItems...), processedBaseItems...)

			// Save the unified items to a single file
			save(fmt.Sprintf("unifiedAIO_%s.dbin.json", arch), allItems)
		}
	}
}
