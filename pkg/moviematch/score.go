package moviematch

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

func ScoreCandidates(hint Hint, candidates []Candidate) []ScoredCandidate {
	scored := make([]ScoredCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		score, reasons := ScoreCandidate(hint, candidate)
		scored = append(scored, ScoredCandidate{Candidate: candidate, Score: score, Reasons: reasons})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Candidate.TMDBID < scored[j].Candidate.TMDBID
		}
		return scored[i].Score > scored[j].Score
	})
	return scored
}

func ScoreCandidate(hint Hint, candidate Candidate) (float64, []string) {
	var reasons []string
	if hint.TMDBID != "" && strconv.Itoa(candidate.TMDBID) == hint.TMDBID {
		return 1, []string{"tmdb_id"}
	}
	if hint.IMDbID != "" && strings.EqualFold(candidate.IMDbID, hint.IMDbID) {
		return 0.99, []string{"imdb_id"}
	}

	titleScore := bestTitleScore(hint.Title, candidate)
	score := titleScore * 0.72
	reasons = append(reasons, fmt.Sprintf("title=%.2f", titleScore))
	if hint.Year > 0 && candidate.Year() > 0 {
		delta := absInt(hint.Year - candidate.Year())
		switch {
		case delta == 0:
			score += 0.22
			reasons = append(reasons, "year=exact")
		case delta == 1:
			score += 0.12
			reasons = append(reasons, "year=near")
		default:
			score -= 0.15
			reasons = append(reasons, "year=mismatch")
		}
	}
	if hint.RuntimeSeconds > 0 && candidate.RuntimeMinutes > 0 {
		deltaMinutes := math.Abs(hint.RuntimeSeconds/60 - float64(candidate.RuntimeMinutes))
		switch {
		case deltaMinutes <= 2:
			score += 0.08
			reasons = append(reasons, "runtime=exact")
		case deltaMinutes <= 10:
			score += 0.04
			reasons = append(reasons, "runtime=near")
		case deltaMinutes >= 30:
			score -= 0.08
			reasons = append(reasons, "runtime=mismatch")
		}
	}
	if candidate.PosterURL != "" {
		score += 0.03
	}
	if candidate.Overview != "" {
		score += 0.03
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score, reasons
}

func bestTitleScore(title string, candidate Candidate) float64 {
	best := StringSimilarity(title, candidate.Title)
	best = math.Max(best, StringSimilarity(title, candidate.OriginalTitle))
	for _, alt := range candidate.AlternativeTitles {
		best = math.Max(best, StringSimilarity(title, alt))
	}
	for _, translated := range candidate.Translations {
		best = math.Max(best, StringSimilarity(title, translated))
	}
	return best
}

func StringSimilarity(a, b string) float64 {
	a = normalizeComparable(a)
	b = normalizeComparable(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	aTokens := strings.Fields(a)
	bTokens := strings.Fields(b)
	tokenScore := jaccard(aTokens, bTokens)
	distance := levenshtein(a, b)
	maxLen := maxInt(len([]rune(a)), len([]rune(b)))
	editScore := 1 - float64(distance)/float64(maxLen)
	return math.Max(tokenScore, editScore)
}

func normalizeComparable(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(".", " ", "_", " ", "-", " ", ":", " ", "'", "", "\"", "", "(", " ", ")", " ", "[", " ", "]", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]struct{})
	setB := make(map[string]struct{})
	for _, v := range a {
		setA[v] = struct{}{}
	}
	for _, v := range b {
		setB[v] = struct{}{}
	}
	intersection := 0
	for v := range setA {
		if _, ok := setB[v]; ok {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	return float64(intersection) / float64(union)
}

func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 0
			if ar[i-1] != br[j-1] {
				cost = 1
			}
			cur[j] = minInt(minInt(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
