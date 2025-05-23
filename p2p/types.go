package p2p

type NodeType string

const (
	ValidatorNode     NodeType = "validator"
	initValidatorNode NodeType = "init_validator"
	SeedsNode         NodeType = "seed"
	initSeedNode      NodeType = "init_seed"
)
