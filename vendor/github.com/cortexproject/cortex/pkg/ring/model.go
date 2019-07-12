package ring

import (
	"fmt"
	"sort"
	"time"

	"github.com/golang/protobuf/proto"
)

// ByToken is a sortable list of TokenDescs
type ByToken []TokenDesc

func (ts ByToken) Len() int           { return len(ts) }
func (ts ByToken) Swap(i, j int)      { ts[i], ts[j] = ts[j], ts[i] }
func (ts ByToken) Less(i, j int) bool { return ts[i].Token < ts[j].Token }

// ProtoDescFactory makes new Descs
func ProtoDescFactory() proto.Message {
	return NewDesc()
}

// NewDesc returns an empty ring.Desc
func NewDesc() *Desc {
	return &Desc{
		Ingesters: map[string]IngesterDesc{},
	}
}

// AddIngester adds the given ingester to the ring.
func (d *Desc) AddIngester(id, addr string, tokens []uint32, state IngesterState, normaliseTokens bool) {
	if d.Ingesters == nil {
		d.Ingesters = map[string]IngesterDesc{}
	}

	ingester := IngesterDesc{
		Addr:      addr,
		Timestamp: time.Now().Unix(),
		State:     state,
	}

	if normaliseTokens {
		ingester.Tokens = tokens
	} else {
		for _, token := range tokens {
			d.Tokens = append(d.Tokens, TokenDesc{
				Token:    token,
				Ingester: id,
			})
		}
		sort.Sort(ByToken(d.Tokens))
	}

	d.Ingesters[id] = ingester
}

// RemoveIngester removes the given ingester and all its tokens.
func (d *Desc) RemoveIngester(id string) {
	delete(d.Ingesters, id)
	output := []TokenDesc{}
	for i := 0; i < len(d.Tokens); i++ {
		if d.Tokens[i].Ingester != id {
			output = append(output, d.Tokens[i])
		}
	}
	d.Tokens = output
}

// ClaimTokens transfers all the tokens from one ingester to another,
// returning the claimed token.
func (d *Desc) ClaimTokens(from, to string, normaliseTokens bool) []uint32 {
	var result []uint32

	if normaliseTokens {

		// If the ingester we are claiming from is normalising, get its tokens then erase them from the ring.
		if fromDesc, found := d.Ingesters[from]; found {
			result = fromDesc.Tokens
			fromDesc.Tokens = nil
			d.Ingesters[from] = fromDesc
		}

		// If we are storing the tokens in a normalise form, we need to deal with
		// the migration from denormalised by removing the tokens from the tokens
		// list.
		// When all ingesters are in normalised mode, d.Tokens is empty here
		for i := 0; i < len(d.Tokens); {
			if d.Tokens[i].Ingester == from {
				result = append(result, d.Tokens[i].Token)
				d.Tokens = d.Tokens[:i+copy(d.Tokens[i:], d.Tokens[i+1:])]
				continue
			}
			i++
		}

		sort.Sort(uint32s(result))
		ing := d.Ingesters[to]
		ing.Tokens = result
		d.Ingesters[to] = ing

	} else {

		for i := 0; i < len(d.Tokens); i++ {
			if d.Tokens[i].Ingester == from {
				d.Tokens[i].Ingester = to
				result = append(result, d.Tokens[i].Token)
			}
		}
	}

	return result
}

// FindIngestersByState returns the list of ingesters in the given state
func (d *Desc) FindIngestersByState(state IngesterState) []IngesterDesc {
	var result []IngesterDesc
	for _, ing := range d.Ingesters {
		if ing.State == state {
			result = append(result, ing)
		}
	}
	return result
}

// Ready returns no error when all ingesters are active and healthy.
func (d *Desc) Ready(heartbeatTimeout time.Duration) error {
	numTokens := len(d.Tokens)
	for id, ingester := range d.Ingesters {
		if time.Now().Sub(time.Unix(ingester.Timestamp, 0)) > heartbeatTimeout {
			return fmt.Errorf("ingester %s past heartbeat timeout", id)
		} else if ingester.State != ACTIVE {
			return fmt.Errorf("ingester %s in state %v", id, ingester.State)
		}
		numTokens += len(ingester.Tokens)
	}

	if numTokens == 0 {
		return fmt.Errorf("Not ready: no tokens in ring")
	}
	return nil
}

// TokensFor partitions the tokens into those for the given ID, and those for others.
func (d *Desc) TokensFor(id string) (tokens, other []uint32) {
	var takenTokens, myTokens []uint32
	for _, token := range migrateRing(d) {
		takenTokens = append(takenTokens, token.Token)
		if token.Ingester == id {
			myTokens = append(myTokens, token.Token)
		}
	}
	return myTokens, takenTokens
}
