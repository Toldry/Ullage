package main

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"

	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/scanner/scannertest"
)

const megabyte = 1 << 20

// scanSample builds a synthetic PC that mixes a developer's, a video editor's and an
// everyday user's data, so estimates are not tuned to one kind of user.
func scanSample(library *recipes.Library, profile string) (*scanner.Result, error) {
	random := rand.New(rand.NewSource(42))
	fileSystem := scannertest.New()
	add := func(path string, megabytes int) {
		fileSystem.AddFile(filepath.ToSlash(profile)+"/"+path, int64(megabytes)*megabyte,
			0)
	}
	for project := 0; project < 35; project++ {
		name := fmt.Sprintf("projects/app-%02d", project)
		for module := 0; module < 6; module++ {
			add(fmt.Sprintf("%s/node_modules/pkg-%d/dist/bundle.js", name, module),
				20+random.Intn(200))
		}
		add(name+"/build/output.bin", 50+random.Intn(400))
		add(name+"/src/main.ts", 1)
	}
	for year := 2015; year <= 2025; year++ {
		for month := 1; month <= 12; month++ {
			for photo := 0; photo < 30; photo++ {
				add(fmt.Sprintf("Pictures/%d/%02d/IMG_%04d.JPG", year, month, photo), 4)
			}
		}
	}
	for clip := 0; clip < 60; clip++ {
		add(fmt.Sprintf("Videos/Captures/stream-%03d.mp4", clip), 800+random.Intn(3000))
	}
	for project := 0; project < 8; project++ {
		name := fmt.Sprintf("Videos/Edits/client-%d", project)
		add(name+"/project.prproj", 30)
		for render := 0; render < 15; render++ {
			add(fmt.Sprintf("%s/Render Files/render-%02d.mov", name, render), 300)
		}
		add(name+"/Exports/final.mp4", 2000)
	}
	for download := 0; download < 120; download++ {
		add(fmt.Sprintf("Downloads/download-%03d.zip", download), 5+random.Intn(900))
	}
	for document := 0; document < 400; document++ {
		add(fmt.Sprintf("Documents/Case Files/case-%03d/brief.pdf", document/10), 3)
	}
	for app := 0; app < 25; app++ {
		add(fmt.Sprintf("AppData/Local/App%02d/data/blob.db", app), 50+random.Intn(2000))
	}
	add("AppData/Local/Temp/huge-installer.exe", 3000)
	add("AppData/Local/npm-cache/_cacache/blob", 800)
	return scanner.Start(context.Background(), scanner.Options{
		Roots:      []string{filepath.FromSlash("/c")},
		Reader:     fileSystem,
		Classifier: classifierFor(library, profile),
	}).Wait()
}
