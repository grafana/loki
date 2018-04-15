package ring

import (
	"sort"
	"time"

	"github.com/golang/protobuf/proto"
)

// ByToken is a sortable list of TokenDescs
type ByToken []*TokenDesc

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
		Ingesters: map[string]*IngesterDesc{},
	}
}

// AddIngester adds the given ingester to the ring.
func (d *Desc) AddIngester(id, addr string, tokens []uint32, state IngesterState) {
	if d.Ingesters == nil {
		d.Ingesters = map[string]*IngesterDesc{}
	}
	d.Ingesters[id] = &IngesterDesc{
		Addr:      addr,
		Timestamp: time.Now().Unix(),
		State:     state,
	}

	for _, token := range tokens {
		d.Tokens = append(d.Tokens, &TokenDesc{
			Token:    token,
			Ingester: id,
		})
	}

	sort.Sort(ByToken(d.Tokens))
}

// RemoveIngester removes the given ingester and all its tokens.
func (d *Desc) RemoveIngester(id string) {
	delete(d.Ingesters, id)
	output := []*TokenDesc{}
	for i := 0; i < len(d.Tokens); i++ {
		if d.Tokens[i].Ingester != id {
			output = append(output, d.Tokens[i])
		}
	}
	d.Tokens = output
}

// ClaimTokens transfers all the tokens from one ingester to another,
// returning the claimed token.
func (d *Desc) ClaimTokens(from, to string) []uint32 {
	var result []uint32
	for i := 0; i < len(d.Tokens); i++ {
		if d.Tokens[i].Ingester == from {
			d.Tokens[i].Ingester = to
			result = append(result, d.Tokens[i].Token)
		}
	}
	return result
}

// FindIngestersByState returns the list of ingesters in the given state
func (d *Desc) FindIngestersByState(state IngesterState) []*IngesterDesc {
	var result []*IngesterDesc
	for _, ing := range d.Ingesters {
		if ing.State == state {
			result = append(result, ing)
		}
	}
	return result
}

// Ready is true when all ingesters are active and healthy.
func (d *Desc) Ready(heartbeatTimeout time.Duration) bool {
	for _, ingester := range d.Ingesters {
		if time.Now().Sub(time.Unix(ingester.Timestamp, 0)) > heartbeatTimeout {
			return false
		} else if ingester.State != ACTIVE {
			return false
		}
	}

	return len(d.Tokens) > 0
}

// TokensFor partitions the tokens into those for the given ID, and those for others.
func (d *Desc) TokensFor(id string) (tokens, other []uint32) {
	var takenTokens, myTokens []uint32
	for _, token := range d.Tokens {
		takenTokens = append(takenTokens, token.Token)
		if token.Ingester == id {
			myTokens = append(myTokens, token.Token)
		}
	}
	return myTokens, takenTokens
}
