package tendermint

/*
Check upon condition on line 36:

	36: upon {PROPOSAL, h_p, round_p, v, ∗} from proposer(h_p, round_p) AND 2f + 1 {PREVOTE, h_p, round_p, id(v)} while
		valid(v) ∧ step_p ≥ prevote for the first time do
	37: if step_p = prevote then
	38: 	lockedValue_p ← v
	39: 	lockedRound_p ← round_p
	40: 	broadcast {PRECOMMIT, h_p, round_p, id(v))}
	41: 	step_p ← precommit
	42: validValue_p ← v
	43: validRound_p ← round_p
*/
func (t *Tendermint[V, H, A]) uponProposalAndPolkaCurrent(cachedProposal *CachedProposal[V, H, A]) bool {
	hasQuorum := t.checkQuorumPrevotesGivenProposalVID(t.state.round, *cachedProposal.ID)
	firstTime := !t.state.lockedValueAndOrValidValueSet
	return hasQuorum &&
		cachedProposal.Valid &&
		t.state.step >= prevote &&
		firstTime
}

func (t *Tendermint[V, H, A]) doProposalAndPolkaCurrent(cachedProposal *CachedProposal[V, H, A]) {
	if t.state.step == prevote {
		t.state.lockedValue = cachedProposal.Value
		t.state.lockedRound = t.state.round
		t.sendPrecommit(cachedProposal.ID)
	}

	t.state.validValue = cachedProposal.Value
	t.state.validRound = t.state.round
	t.state.lockedValueAndOrValidValueSet = true
}
