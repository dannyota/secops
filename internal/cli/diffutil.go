package cli

import "strings"

// unifiedDiff returns a line-based diff of a vs b with '-'/'+'/' ' markers and a
// header, computed via a longest-common-subsequence walk. Dependency-free and
// sufficient for YARA-L rule text (small); it prints full context rather than
// hunk windows, which reads cleanly for rules. O(n*m) memory — fine at rule size.
func unifiedDiff(a, b, labelA, labelB string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	n, m := len(al), len(bl)
	// dp[i][j] = LCS length of al[i:] and bl[j:].
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var sb strings.Builder
	sb.WriteString("--- " + labelA + "\n+++ " + labelB + "\n")
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case al[i] == bl[j]:
			sb.WriteString("  " + al[i] + "\n")
			i, j = i+1, j+1
		case dp[i+1][j] >= dp[i][j+1]:
			sb.WriteString("- " + al[i] + "\n")
			i++
		default:
			sb.WriteString("+ " + bl[j] + "\n")
			j++
		}
	}
	for ; i < n; i++ {
		sb.WriteString("- " + al[i] + "\n")
	}
	for ; j < m; j++ {
		sb.WriteString("+ " + bl[j] + "\n")
	}
	return sb.String()
}
