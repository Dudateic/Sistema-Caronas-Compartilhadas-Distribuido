package distribuido

type TipoMensagemRA string

const (
	MsgRequest RA_Tipo = "REQUEST"
	MsgReply   RA_Tipo = "REPLY"
)

type RA_Tipo string

type MensagemDistribuida struct {
	Tipo      RA_Tipo `json:"tipo"`      // "REQUEST" ou "REPLY"
	OrigemID  string  `json:"origem_id"` // Identificador da empresa (ex: "empresa_A")
	Timestamp int64   `json:"Lamport"`
	RecursoID string  `json:"recurso_id"` // Identificador do recurso disputado (ex: "carona_12_trecho_1")
}
