package results

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/toldry/ullage/internal/scanner"
)

// placeholderToken matches <R3> and <R3>.pdf in text the assistant wrote.
var placeholderToken = regexp.MustCompile(`<R\d+>(\.[A-Za-z0-9]{1,10})?`)

// restorer puts real names back into the assistant's text for local display. It only
// knows the placeholders of items in this export and their folders, so it cannot reveal
// a name the user has not already seen in their own scan.
type restorer struct {
	names map[string]string
}

func newRestorer(input Input) *restorer {
	result := &restorer{names: map[string]string{}}
	if input.Export == nil || input.Redactor == nil {
		return result
	}
	seen := map[scanner.NodeIndex]bool{}
	for _, item := range input.Export.Items {
		for index := item.Node; index != scanner.NoNode && !seen[index]; {
			seen[index] = true
			node := input.Tree.Node(index)
			if node.Parent == scanner.NoNode {
				break
			}
			result.learn(input.Redactor.RenderName(index), node.Name)
			index = node.Parent
		}
	}
	return result
}

func (restorer *restorer) learn(rendered, real string) {
	if !strings.HasPrefix(rendered, "<R") {
		return
	}
	restorer.names[rendered] = real
	// "<R3>.pdf" also teaches "<R3>", which the assistant may write without the extension.
	if closing := strings.Index(rendered, ">"); closing+1 < len(rendered) {
		stem, extension := real, rendered[closing+1:]
		if len(real) > len(extension) &&
			strings.EqualFold(real[len(real)-len(extension):], extension) {
			stem = real[:len(real)-len(extension)]
		}
		if _, known := restorer.names[rendered[:closing+1]]; !known {
			restorer.names[rendered[:closing+1]] = stem
		}
	}
}

func (restorer *restorer) restore(text string) string {
	if len(restorer.names) == 0 {
		return text
	}
	return placeholderToken.ReplaceAllStringFunc(text, func(token string) string {
		if real, known := restorer.names[token]; known {
			return real
		}
		bare := token[:strings.Index(token, ">")+1]
		if real, known := restorer.names[bare]; known {
			return real + token[len(bare):]
		}
		return token
	})
}

func itoa(value int) string { return strconv.Itoa(value) }
