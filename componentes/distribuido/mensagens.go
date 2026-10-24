package distribuido

type TipoMensagemRA string

const (
	MsgRequest TipoMensagemRA = "REQUEST"
	MsgReply   TipoMensagemRA = "REPLY"
	MsgPing    TipoMensagemRA = "PING"
	MsgPong    TipoMensagemRA = "PONG"
)

type MensagemDistribuida struct {
	Tipo      TipoMensagemRA `json:"tipo"`
	OrigemID  string         `json:"origem_id"`
	Timestamp int64          `json:"Lamport"`
	RecursoID string         `json:"recurso_id,omitempty"`
}
