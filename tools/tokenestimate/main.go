// Command tokenestimate is a DEVELOPMENT tool: it builds the prompts for a sample scan
// and estimates their token counts offline. It is not part of the shipped executable
// (only cmd/ullage is built for release) and it makes no network calls.
//
// Usage:
//
//	go run ./tools/tokenestimate             # synthetic sample of a typical PC
//	go run ./tools/tokenestimate -root DIR   # scan a real folder (read-only)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"unicode"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/prompt"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/tiers"
)

func main() {
	root := flag.String("root", "", "scan this folder instead of the synthetic sample")
	show := flag.Bool("show", false, "print batch 1")
	flag.Parse()
	if err := run(*root, *show); err != nil {
		fmt.Fprintln(os.Stderr, "tokenestimate:", err)
		os.Exit(1)
	}
}

func run(root string, show bool) error {
	library, err := recipes.Load()
	if err != nil {
		return err
	}
	var result *scanner.Result
	var profile string
	if root == "" {
		profile = filepath.FromSlash("/c/Users/sam")
		result, err = scanSample(library, profile)
	} else {
		profile, _ = os.UserHomeDir()
		result, err = scanFolder(library, root)
	}
	if err != nil {
		return err
	}
	redactor := redact.New(redact.Options{Tree: result.Tree,
		Registry: redact.NewRegistry(nil, 1, true),
		Context:  redact.Context{CurrentProfile: profile, CaseInsensitive: true}})
	export := prompt.Build(result.Tree, redactor, "7KQX2M", prompt.DefaultOptions)

	fmt.Printf("%d items in %d batches; %s of %s Unknown represented\n", len(export.Items),
		len(export.Batches), megabytes(export.RepresentedBytes), megabytes(export.UnknownBytes))
	fmt.Printf("%-6s %6s %8s %10s %10s\n", "batch", "lines", "chars", "tokens≈", "chars/4")
	var total int
	for _, batch := range export.Batches {
		tokens := EstimateTokens(batch.Text)
		total += tokens
		fmt.Printf("%-6d %6d %8d %10d %10d\n", batch.Number, batch.Lines, len(batch.Text),
			tokens, len(batch.Text)/4)
	}
	fmt.Printf("total tokens≈ %d\n", total)
	if show && len(export.Batches) > 0 {
		fmt.Println("\n" + export.Batches[0].Text)
	}
	return nil
}

// EstimateTokens approximates a BPE tokenizer without any vocabulary: a letter run costs
// one token per six letters (at least one), a digit run one per three digits, and every
// other visible character one token. Expect roughly ±30%; it leans toward
// overestimating, which is the safe side for staying under a model's limits.
func EstimateTokens(text string) int {
	tokens := 0
	runes := []rune(text)
	for index := 0; index < len(runes); {
		character := runes[index]
		switch {
		case unicode.IsLetter(character):
			end := index
			for end < len(runes) && unicode.IsLetter(runes[end]) {
				end++
			}
			// Common words are usually one token; long or rare ones split into several.
			tokens += (end - index + 5) / 6
			index = end
		case unicode.IsDigit(character):
			end := index
			for end < len(runes) && unicode.IsDigit(runes[end]) {
				end++
			}
			tokens += (end - index + 2) / 3
			index = end
		case character == ' ' || character == '\t':
			index++
		default:
			tokens++
			index++
		}
	}
	return tokens
}

func megabytes(bytes int64) string {
	return strconv.FormatInt(bytes>>20, 10) + " MiB"
}

func classifierFor(library *recipes.Library, profile string) *tiers.Classifier {
	locations := platform.KnownLocations{
		Variables: map[string][]string{"UserProfile": {profile},
			"CurrentUserProfile": {profile}, "SystemRoot": {filepath.FromSlash("/c/Windows")},
			"AnyVolumeRoot": {filepath.FromSlash("/c")}},
		CaseInsensitive: true,
		PathSeparator:   filepath.Separator,
	}
	return tiers.New(library, locations, "windows", nil)
}

func scanFolder(library *recipes.Library, root string) (*scanner.Result, error) {
	home, _ := os.UserHomeDir()
	return scanner.Start(context.Background(), scanner.Options{Roots: []string{root},
		Reader: platform.NewDirectoryReader(), Classifier: classifierFor(library, home)}).Wait()
}
