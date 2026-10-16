package symboldiff

import (
	"fmt"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/uir"
)

func (history History) Pretty() api.Text {
	text := api.Text{}.Append(history.RootKey, uir.StyleBold).Append(" · history", uir.StyleDim).
		Append("\nCheckout  ").Append(history.Location, uir.StyleFileName).Append("\n")
	text = text.Append("Branches\n", uir.StyleBold)
	if len(history.Branches) == 0 {
		text = text.Append("  No branches\n", uir.StyleMuted)
	}
	for _, branch := range history.Branches {
		text = text.Append("  ").Append(branch.Name, uir.StyleName).Append("  ").
			Append(abbreviate(branch.Commit, 12), uir.StyleDim).Append("\n")
	}
	text = text.Append("Pull requests\n", uir.StyleBold)
	switch {
	case history.PullRequestError != "":
		text = text.Append("  unavailable: "+history.PullRequestError+"\n", uir.StyleWarning)
	case len(history.PullRequests) == 0:
		text = text.Append("  No pull requests\n", uir.StyleMuted)
	}
	for _, request := range history.PullRequests {
		text = text.Append("  ").Append(fmt.Sprintf("#%d", request.Number), uir.StyleName).
			Append("  ").Append(abbreviate(request.Commit, 12), uir.StyleDim).Append("\n")
	}
	text = text.Append("Commits\n", uir.StyleBold)
	if len(history.Commits) == 0 {
		return text.Append("  No commits\n", uir.StyleMuted)
	}
	for _, commit := range history.Commits {
		text = text.Append("  ").Append(abbreviate(commit.Commit, 12), uir.StyleName).
			Append("  ").Append(commit.Subject).Append("\n    ").Append(commit.AuthoredAt, uir.StyleDim)
		if commit.SnapshotID != "" {
			text = text.Append("  ").Append("indexed", uir.StyleName)
			if commit.SnapshotReason != "" {
				text = text.Append(" ("+commit.SnapshotReason+")", uir.StyleDim)
			}
			text = text.Append("  ").Append(abbreviate(commit.SnapshotID, 13), uir.StyleDim)
		}
		text = text.Append("\n")
	}
	return text
}

func abbreviate(value string, width int) string {
	if len(value) > width {
		return value[:width]
	}
	return value
}
