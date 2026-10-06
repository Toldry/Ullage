package prompt

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/scanner"
)

// Batch is one prompt the user copies.
type Batch struct {
	Number  int    `json:"number"`
	FirstID int    `json:"firstId"`
	LastID  int    `json:"lastId"`
	Lines   int    `json:"lines"`
	Items   int    `json:"items"`
	Text    string `json:"text"`
}

// Export is the full set of prompts for one scan and redaction state.
type Export struct {
	Code             string
	Items            []Item
	Batches          []Batch
	RepresentedBytes int64
	UnknownBytes     int64
}

// ItemByID returns the item for an ID, or nil.
func (export *Export) ItemByID(id int) *Item {
	if id < 1 || id > len(export.Items) {
		return nil
	}
	return &export.Items[id-1]
}

// BatchOf returns the batch number containing the ID, or 0.
func (export *Export) BatchOf(id int) int {
	for _, batch := range export.Batches {
		if id >= batch.FirstID && id <= batch.LastID {
			return batch.Number
		}
	}
	return 0
}

// entry is one data line: a single item, or several same-named folders collapsed.
type entry struct {
	items    []int
	parent   []string
	name     string
	grouped  bool
	sortPath string
}

// Build selects, orders, numbers and batches the Unknown tier.
func Build(tree *scanner.Tree, redactor *redact.Redactor, code string,
	options Options) Export {
	items, represented := selectItems(tree, redactor, options)
	var unknown int64
	for _, root := range tree.Roots {
		unknown += tree.Node(root).TierBytes(scanner.TierUnknown)
	}
	entries := buildEntries(items, redactor, options)
	export := Export{Code: code, RepresentedBytes: represented, UnknownBytes: unknown}
	nextID := 1
	for _, entry := range entries {
		for _, index := range entry.items {
			items[index].ID = nextID
			nextID++
		}
	}
	export.Items = make([]Item, len(items))
	for _, item := range items {
		export.Items[item.ID-1] = item
	}
	export.Batches = buildBatches(entries, items, code, options)
	return export
}

func splitRendered(rendered string) []string {
	return strings.Split(rendered, "/")
}

func buildEntries(items []Item, redactor *redact.Redactor, options Options) []entry {
	type candidate struct {
		segments []string
		index    int
	}
	groups := map[string][]candidate{}
	var groupOrder []string
	var entries []entry
	for index, item := range items {
		segments := splitRendered(redactor.Render(item.Node))
		if item.Kind == ItemLooseContents {
			segments = append(segments, "*")
		}
		if item.Kind == ItemFolder && item.nameShown {
			key := segments[0] + "\x00" + strings.ToLower(segments[len(segments)-1])
			if _, seen := groups[key]; !seen {
				groupOrder = append(groupOrder, key)
			}
			groups[key] = append(groups[key], candidate{segments: segments, index: index})
			continue
		}
		entries = append(entries, singleEntry(segments, index))
	}
	for _, key := range groupOrder {
		members := groups[key]
		if len(members) < options.CollapseMinimum {
			for _, member := range members {
				entries = append(entries, singleEntry(member.segments, member.index))
			}
			continue
		}
		sort.Slice(members, func(first, second int) bool {
			return joinLower(members[first].segments) < joinLower(members[second].segments)
		})
		parent := members[0].segments[:len(members[0].segments)-1]
		grouped := entry{grouped: true, name: members[0].segments[len(members[0].segments)-1]}
		for _, member := range members {
			parent = commonPrefix(parent, member.segments[:len(member.segments)-1])
			grouped.items = append(grouped.items, member.index)
		}
		grouped.parent = parent
		grouped.sortPath = joinLower(append(append([]string{}, parent...), grouped.name))
		entries = append(entries, grouped)
	}
	// Sorting by path puts related items next to each other, so responses can use ranges.
	sort.SliceStable(entries, func(first, second int) bool {
		return entries[first].sortPath < entries[second].sortPath
	})
	return entries
}

func singleEntry(segments []string, index int) entry {
	return entry{items: []int{index}, parent: segments[:len(segments)-1],
		name: segments[len(segments)-1], sortPath: joinLower(segments)}
}

func joinLower(segments []string) string {
	return strings.ToLower(strings.Join(segments, "/"))
}

func commonPrefix(first, second []string) []string {
	length := 0
	for length < len(first) && length < len(second) && first[length] == second[length] {
		length++
	}
	return first[:length]
}

// maximumRelativeSegments bounds how deep an item may sit below its header line.
const maximumRelativeSegments = 4

func buildBatches(entries []entry, items []Item, code string, options Options) []Batch {
	var batches []Batch
	for start := 0; start < len(entries); start += options.BatchLines {
		end := min(start+options.BatchLines, len(entries))
		batches = append(batches, Batch{Number: len(batches) + 1})
		batch := &batches[len(batches)-1]
		batch.FirstID = items[entries[start].items[0]].ID
		last := entries[end-1].items
		batch.LastID = items[last[len(last)-1]].ID
		batch.Lines = end - start
		batch.Items = batch.LastID - batch.FirstID + 1
		batch.Text = renderData(entries[start:end], items)
	}
	if len(batches) == 0 {
		batches = append(batches, Batch{Number: 1})
	}
	for index := range batches {
		batches[index].Text = wrapBatch(batches[index], len(batches), code)
	}
	return batches
}

// renderData lays entries out under header lines that hold shared path prefixes.
func renderData(entries []entry, items []Item) string {
	var builder strings.Builder
	for start := 0; start < len(entries); {
		header, end := headerRun(entries, start)
		builder.WriteString(renderHeader(header) + "\n")
		for _, current := range entries[start:end] {
			builder.WriteString(renderLine(current, header, items) + "\n")
		}
		start = end
	}
	return builder.String()
}

// headerRun finds the header for entries[start:] and how many entries share it. It
// extends a run while the header stays deep enough and items stay close below it.
func headerRun(entries []entry, start int) ([]string, int) {
	header := entries[start].parent
	end := start + 1
	for end < len(entries) {
		candidate := commonPrefix(header, entries[end].parent)
		if len(candidate) < len(header)-1 || len(candidate) == 0 ||
			!allWithinReach(entries[start:end+1], candidate) {
			break
		}
		header = candidate
		end++
	}
	return header, end
}

func allWithinReach(entries []entry, header []string) bool {
	for _, current := range entries {
		if len(current.parent)-len(header)+1 > maximumRelativeSegments {
			return false
		}
	}
	return true
}

func renderHeader(header []string) string {
	text := strings.Join(header, "/")
	if text == "" {
		return "/"
	}
	return text
}

func renderLine(current entry, header []string, items []Item) string {
	relative := strings.Join(append(append([]string{}, current.parent[len(header):]...),
		current.name), "/")
	first := items[current.items[0]]
	if !current.grouped {
		return " " + strconv.Itoa(first.ID) + " " + formatSize(first.Bytes) + " " + relative +
			details(first.Files, first.modified, first.created, first.types, first.nameShown)
	}
	var bytes, files, newest, oldest int64
	var summaries [][]scanner.ExtensionShare
	last := first
	for _, index := range current.items {
		member := items[index]
		bytes += member.Bytes
		files += member.Files
		newest = max(newest, member.modified)
		if member.created > 0 && (oldest == 0 || member.created < oldest) {
			oldest = member.created
		}
		summaries = append(summaries, member.types)
		last = member
	}
	return fmt.Sprintf(" %d-%d %s %s x%d", first.ID, last.ID, formatSize(bytes), relative,
		len(current.items)) +
		details(files, newest, oldest, scanner.MergeExtensions(summaries, 3), true)
}
