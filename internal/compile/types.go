package compile

// RewordClaim is an existing claim that stays active but is restated.
type RewordClaim struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// RetireClaim is an existing claim being marked superseded.
type RetireClaim struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// AddClaim is a newly observed claim, with a body-line span where one could be
// determined.
type AddClaim struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	Span   []int  `json:"span,omitempty"`
}

// ClaimsChange is the decoded, validated shape of one extract_claims response. Its
// field names and nesting match 04-technical-spec-v1.md section 3.5's
// change.claims{keep,reword,retire,add} exactly, so BuildProposal needs no
// reshaping.
type ClaimsChange struct {
	Keep   []string      `json:"keep"`
	Reword []RewordClaim `json:"reword"`
	Retire []RetireClaim `json:"retire"`
	Add    []AddClaim    `json:"add"`
}
