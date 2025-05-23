package p2p

type NodeType string

const (
	SeedsNode     NodeType = "seed"
	ValidatorNode NodeType = "validator"
	RegularNode   NodeType = "regular"
	APINode       NodeType = "api"
)
