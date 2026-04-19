package evaluation

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
