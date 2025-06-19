package p2p

type NodeType string

const (
	SeedsNode     NodeType = "seed"
	ValidatorNode NodeType = "validator"
	FullNode      NodeType = "full"
	APINode       NodeType = "api"
)
