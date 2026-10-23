package scanner

import "github.com/snonux/player/internal/model"

// IdentityPair is a deliberate missing→new assignment, or an unresolved conflict.
type IdentityPair struct {
	Missing    model.Media
	New        model.Media
	Kind       MatchKind
	Ambiguous  bool
	Candidates []model.Media // set when Ambiguous
}

type scoredEdge struct {
	mi, ci int
	kind   MatchKind
}

// PairIdentities assigns soft-deleted audio rows to newly created ones.
// Ambiguous best-score ties become conflicts (Ambiguous=true, New zero).
func PairIdentities(missing, created []model.Media) []IdentityPair {
	if len(missing) == 0 || len(created) == 0 {
		return nil
	}
	var edges []scoredEdge
	for mi, m := range missing {
		mid := IdentityOf(m)
		for ci, c := range created {
			kind := ClassifyMatch(mid, IdentityOf(c))
			if kind == MatchNone {
				continue
			}
			edges = append(edges, scoredEdge{mi, ci, kind})
		}
	}
	usedM := make([]bool, len(missing))
	usedC := make([]bool, len(created))
	var pairs []IdentityPair

	for _, minKind := range []MatchKind{MatchExact, MatchStrong, MatchStem} {
		pairs = append(pairs, assignAtRank(missing, created, edges, usedM, usedC, minKind)...)
	}

	var leftM, leftC []int
	for i := range missing {
		if !usedM[i] {
			leftM = append(leftM, i)
		}
	}
	for i := range created {
		if !usedC[i] {
			leftC = append(leftC, i)
		}
	}
	if len(leftM) == 1 && len(leftC) == 1 &&
		SameTagFingerprint(IdentityOf(missing[leftM[0]]), IdentityOf(created[leftC[0]])) {
		pairs = append(pairs, IdentityPair{
			Missing: missing[leftM[0]],
			New:     created[leftC[0]],
			Kind:    MatchStrong,
		})
		usedM[leftM[0]] = true
		usedC[leftC[0]] = true
	}
	return pairs
}

func assignAtRank(missing, created []model.Media, edges []scoredEdge, usedM, usedC []bool, minKind MatchKind) []IdentityPair {
	var pairs []IdentityPair
	for {
		var active []scoredEdge
		for _, e := range edges {
			if e.kind < minKind || usedM[e.mi] || usedC[e.ci] {
				continue
			}
			active = append(active, e)
		}
		if len(active) == 0 {
			return pairs
		}
		top := MatchNone
		for _, e := range active {
			if e.kind > top {
				top = e.kind
			}
		}
		byMissing := map[int][]scoredEdge{}
		byCreated := map[int][]scoredEdge{}
		for _, e := range active {
			if e.kind != top {
				continue
			}
			byMissing[e.mi] = append(byMissing[e.mi], e)
			byCreated[e.ci] = append(byCreated[e.ci], e)
		}
		progress := false
		for mi, group := range byMissing {
			if usedM[mi] {
				continue
			}
			if len(group) != 1 {
				cands := uniqueCreated(created, group)
				if len(cands) > 1 {
					pairs = append(pairs, IdentityPair{
						Missing: missing[mi], Ambiguous: true, Candidates: cands, Kind: top,
					})
					usedM[mi] = true
					progress = true
				}
				continue
			}
			e := group[0]
			if usedC[e.ci] || len(byCreated[e.ci]) != 1 {
				if len(byCreated[e.ci]) > 1 && !usedM[mi] {
					// Multiple missings claim this created at the same rank: conflict on created side → leave unmatched this round.
					continue
				}
				continue
			}
			pairs = append(pairs, IdentityPair{Missing: missing[e.mi], New: created[e.ci], Kind: e.kind})
			usedM[e.mi] = true
			usedC[e.ci] = true
			progress = true
		}
		if !progress {
			// Remaining top-rank edges are tangled; mark each unused missing with >1 candidate as conflict.
			for mi, group := range byMissing {
				if usedM[mi] {
					continue
				}
				cands := uniqueCreated(created, group)
				if len(cands) > 1 {
					pairs = append(pairs, IdentityPair{
						Missing: missing[mi], Ambiguous: true, Candidates: cands, Kind: top,
					})
					usedM[mi] = true
					progress = true
				}
			}
			if !progress {
				return pairs
			}
		}
	}
}

func uniqueCreated(created []model.Media, group []scoredEdge) []model.Media {
	seen := map[int]bool{}
	var out []model.Media
	for _, e := range group {
		if seen[e.ci] {
			continue
		}
		seen[e.ci] = true
		out = append(out, created[e.ci])
	}
	return out
}
