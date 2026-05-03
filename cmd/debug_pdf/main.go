package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: debug_pdf <file.pdf>")
		return
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer f.Close()

	stat, _ := f.Stat()
	pdfReader, err := pdf.NewReader(f, stat.Size())
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	for pageNum := 1; pageNum <= pdfReader.NumPage(); pageNum++ {
		fmt.Printf("\n=== PAGE %d ===\n", pageNum)
		page := pdfReader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		texts := page.Content().Text

		// Sort by Y (descending), then X
		sort.Slice(texts, func(i, j int) bool {
			if texts[i].Y != texts[j].Y {
				return texts[i].Y > texts[j].Y
			}
			return texts[i].X < texts[j].X
		})

		// Group into lines and reconstruct
		var currentY float64 = -1
		var lineBuilder strings.Builder
		
		for _, t := range texts {
			if currentY < 0 {
				currentY = t.Y
			} else if currentY-t.Y > 3 {
				// Print the completed line with character positions
				line := lineBuilder.String()
				if len(line) > 50 {
					fmt.Printf("Y=%.0f len=%d: %s\n", currentY, len(line), line)
				}
				lineBuilder.Reset()
				currentY = t.Y
			}
			lineBuilder.WriteString(t.S)
		}
		// Print last line
		line := lineBuilder.String()
		if len(line) > 50 {
			fmt.Printf("Y=%.0f len=%d: %s\n", currentY, len(line), line)
		}
	}
}
