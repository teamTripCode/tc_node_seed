package p2p

type NodeType string

const (
	SeedsNode     NodeType = "seed"
	ValidatorNode NodeType = "validator"
	FullNode      NodeType = "full" // Or RegularNode, ensure consistency later
	APINode       NodeType = "api"
)

// NodeUpdateNotification is the structure for notifying other nodes about a new node.
type NodeUpdateNotification struct {
	Address  string   `json:"address"`
	NodeType NodeType `json:"nodeType"`
}
