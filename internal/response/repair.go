package response

import (
	"fmt"
	"strings"
)

const maximumListedProblems = 20

// RepairPrompt asks for the full corrected reply, listing each problem concisely. It is
// pasted into the same conversation, so it does not repeat the instructions.
func RepairPrompt(result Result, expectation Expectation) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Your reply for batch %d of %d (session %s) has problems:\n",
		expectation.Batch, expectation.BatchCount, expectation.Code)
	for index, problem := range result.Problems {
		if index == maximumListedProblems {
			fmt.Fprintf(&builder, "- and %d more\n", len(result.Problems)-index)
			break
		}
		builder.WriteString("- ")
		if problem.Subject != "" {
			fmt.Fprintf(&builder, "row %q: ", problem.Subject)
		}
		builder.WriteString(problem.Message + "\n")
	}
	fmt.Fprintf(&builder, "Send the full corrected reply for batch %d only, from "+
		"@SESSION %s to @END %s, with nothing before or after it.\n", expectation.Batch,
		expectation.Code, expectation.Code)
	return builder.String()
}
