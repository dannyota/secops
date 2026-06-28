package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// ruleTokenRe matches the `rule <identifier>` declaration at the start of a line —
// the rule's name token, which for a custom rule is also its display name.
var ruleTokenRe = regexp.MustCompile(`(?m)^(\s*rule\s+)([A-Za-z_][A-Za-z0-9_]*)`)

// newRulesDuplicateCmd clones an existing rule's YARA-L under a new name (the
// console's "Duplicate Rule"). The clone is created live but DISABLED and
// non-alerting (a new rule has no deployment until you deploy it), so it is safe.
// Guarded: dry-run by default, --yes to apply.
func newRulesDuplicateCmd() *cobra.Command {
	var (
		name        string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "duplicate <rule> [--name NEW]",
		Short: "MUTATING (guarded): clone a rule's YARA-L under a new name (created disabled)",
		Long: "Copy an existing rule's YARA-L into a new rule with a new name token — the\n" +
			"console's Duplicate Rule. The clone is created DISABLED and non-alerting (a\n" +
			"fresh rule has no deployment); deploy it with `rules promote`/`push rules-deploy`\n" +
			"when ready. <rule> is an id, display name, or slug. --name sets the new rule's\n" +
			"name (default: the source name + \"_copy\"). Re-run `pull rules` to mirror the\n" +
			"clone. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			ruleID, err := resolveRuleID(ctx, c, args[0])
			if err != nil {
				return err
			}
			src, err := c.GetRule(ctx, ruleID)
			if err != nil {
				return err
			}
			if strings.TrimSpace(src.Text) == "" {
				return fmt.Errorf("source rule %s has no YARA-L text to clone", ruleID)
			}

			newText, oldToken := rewriteRuleToken(src.Text, "")
			if oldToken == "" {
				return fmt.Errorf("could not find the `rule <name>` declaration in the source YARA-L")
			}
			newToken := toRuleToken(name)
			if newToken == "" {
				newToken = oldToken + "_copy"
			}
			newText, _ = rewriteRuleToken(src.Text, newToken)

			if err := refuseRuleNameCollision(ctx, c, newToken); err != nil {
				return err
			}

			action := fmt.Sprintf("duplicate rule %s as %q (disabled)", ruleID, newToken)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				nr, err := c.CreateRule(ctx, newText)
				if err != nil {
					return err
				}
				if !jsonOut {
					fmt.Fprintf(os.Stdout, "created %s (%s), disabled. Deploy with `rules promote`/`push rules-deploy`; `pull rules` to mirror.\n",
						newToken, nr.RuleID())
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "name for the clone (default: source name + \"_copy\")")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// refuseRuleNameCollision errors if a rule with the given display name already
// exists, so a duplicate doesn't silently create a second same-named rule.
func refuseRuleNameCollision(ctx context.Context, c *chronicle.Client, name string) error {
	rules, err := c.ListRulesBasic(ctx)
	if err != nil {
		return err
	}
	for i := range rules {
		if strings.EqualFold(strings.TrimSpace(rules[i].DisplayName), name) {
			return fmt.Errorf("a rule named %q already exists (%s) — pass --name to choose another", name, rules[i].RuleID())
		}
	}
	return nil
}

// rewriteRuleToken replaces the rule's name token with newToken (empty newToken
// = no rewrite, just probe), returning the new text and the original token.
func rewriteRuleToken(text, newToken string) (out, oldToken string) {
	loc := ruleTokenRe.FindStringSubmatchIndex(text)
	if loc == nil {
		return text, ""
	}
	oldToken = text[loc[4]:loc[5]] // group 2 (the identifier)
	if newToken == "" {
		return text, oldToken
	}
	return text[:loc[4]] + newToken + text[loc[5]:], oldToken
}

// toRuleToken sanitizes a free-form name into a valid YARA-L rule identifier
// (letters/digits/underscore, not starting with a digit). Empty input → "".
func toRuleToken(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return ""
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "r_" + s
	}
	return s
}
