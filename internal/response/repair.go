package response

import (
	"fmt"
	"strings"
)

const maximumListedProblems = 20

// RepairPrompt asks for the full corrected reply, listing each problem concisely. It is
// pasted into the same conversation, so it does not repeat the instructions.
func RepairPrompt(result Result, expectation Expectation) string {
	return repairText(fmt.Sprintf("batch %d of %d", expectation.Batch,
		expectation.BatchCount), fmt.Sprintf("batch %d only", expectation.Batch),
		expectation.Code, result.Problems)
}

func repairText(subject, scope, code string, problems []Problem) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Your reply for %s (session %s) has problems:\n", subject, code)
	for index, problem := range problems {
		if index == maximumListedProblems {
			fmt.Fprintf(&builder, "- and %d more\n", len(problems)-index)
			break
		}
		builder.WriteString("- ")
		if problem.Subject != "" {
			fmt.Fprintf(&builder, "row %q: ", problem.Subject)
		}
		builder.WriteString(problem.Message + "\n")
	}
	fmt.Fprintf(&builder, "Send the full corrected reply for %s, from @SESSION %s to "+
		"@END %s, with nothing before or after it.\n", scope, code, code)
	return builder.String()
}
