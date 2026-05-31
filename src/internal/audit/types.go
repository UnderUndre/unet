package audit

type Action string

const (
	ActionCreatePeer  Action = "create_peer"
	ActionDeletePeer  Action = "delete_peer"
	ActionCreateRoute Action = "create_route"
	ActionDeleteRoute Action = "delete_route"
	ActionCreateToken Action = "create_token"
	ActionRevokeToken Action = "revoke_token"
	ActionRotateCert  Action = "rotate_cert"
)

type Entry struct {
	ID               string         `json:"id"`
	Timestamp        string         `json:"timestamp"`
	ActorTokenID     string         `json:"actorTokenId"`
	ActorTokenName   string         `json:"actorTokenName"`
	Action           Action         `json:"action"`
	TargetResourceID string         `json:"targetResourceId"`
	SourceIP         string         `json:"sourceIp"`
	UserAgent        string         `json:"userAgent"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}
