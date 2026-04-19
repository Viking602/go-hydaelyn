package evaluation

import "time"

func ResolveTemporalConflicts(claims []Claim, corpus Corpus) []Claim {
	resolved := make([]Claim, 0, len(claims))
	for _, claim := range claims {
		assessed := assessClaimAgainstCorpus(claim, corpus)
		if assessed.Status == ClaimBlocked {
			resolved = append(resolved, assessed)
			continue
		}

		latestSupport := latestDocumentByID(assessed.SupportDocIDs, corpus)
		latestConflict := latestDocumentByID(assessed.ConflictDocIDs, corpus)
		if latestSupport != nil && latestConflict != nil {
			assessed.Conflict = true
			if latestConflict.Date.After(latestSupport.Date) {
				assessed.Status = ClaimUnsupported
				assessed.Reason = "newer conflicting evidence overrides older support"
				assessed.SupportDocIDs = nil
			} else {
				assessed.Status = ClaimSupported
				assessed.Reason = "newer supporting evidence overrides older conflict"
			}
		}
		resolved = append(resolved, assessed)
	}
	return resolved
}

func latestDocumentByID(ids []string, corpus Corpus) *CorpusDocument {
	var latest *CorpusDocument
	for _, id := range ids {
		for _, doc := range corpus.Documents {
			if doc.ID != id {
				continue
			}
			if latest == nil || doc.Date.After(latest.Date) || (doc.Date.Equal(latest.Date) && doc.ID > latest.ID) {
				copy := doc
				latest = &copy
			}
		}
	}
	return latest
}

func newerOf(left, right *CorpusDocument) *CorpusDocument {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	if right.Date.After(left.Date) || (right.Date.Equal(left.Date) && right.ID > left.ID) {
		return right
	}
	return left
}

func newerTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
