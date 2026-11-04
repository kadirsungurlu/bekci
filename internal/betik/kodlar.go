package betik

// Red kodları: ajan iş reddettiğinde panele yalnız bu kodlardan biri gider
// (docs/BETIK-SOZLESME.md §6). Arayüz metni bu kodlardan üretilir.
const (
	CodeDisabled        = "disabled"
	CodeConfigInvalid   = "config_invalid"
	CodeStateInvalid    = "state_invalid"
	CodeInsecurePanel   = "insecure_panel"
	CodePlatform        = "platform"
	CodeClockBack       = "clock_back"
	CodeEnvelopeInvalid = "envelope_invalid"
	CodeSigInvalid      = "sig_invalid"
	CodeSchemaUnknown   = "schema_unknown"
	CodePayloadInvalid  = "payload_invalid"
	CodeExpired         = "expired"
	CodeNotYet          = "not_yet"
	CodeRevoked         = "revoked"
	CodeVersionRollback = "version_rollback"
	CodeVersionJump     = "version_jump"
	CodeBodyTooLarge    = "body_too_large"
	CodeBodyMismatch    = "body_mismatch"
	CodeGroupDenied     = "group_denied"
	CodeNotAllowed      = "not_allowed"
	CodePinMismatch     = "pin_mismatch"
	CodeTicketMissing   = "ticket_missing"
	CodeTicketSig       = "ticket_sig_invalid"
	CodeTicketInvalid   = "ticket_invalid"
	CodeTicketHost      = "ticket_host"
	CodeTicketMismatch  = "ticket_mismatch"
	CodeTicketExpired   = "ticket_expired"
	CodeParamsInvalid   = "params_invalid"
	CodeRunIDFormat     = "run_id_format"
	CodeRunIDReplay     = "run_id_replay"
	CodeRateInterval    = "rate_interval"
	CodeRateDay         = "rate_day"
	CodeRateHour        = "rate_hour"
	CodeBusy            = "busy"
	CodeStateWrite      = "state_write"
	CodeExecFailed      = "exec_failed"
)

// AllCodes tüm red kodları; arayüz çevirisinin eksiksizliğini denetlemek için.
var AllCodes = []string{
	CodeDisabled, CodeConfigInvalid, CodeStateInvalid, CodeInsecurePanel, CodePlatform, CodeClockBack,
	CodeEnvelopeInvalid, CodeSigInvalid, CodeSchemaUnknown, CodePayloadInvalid, CodeExpired, CodeNotYet,
	CodeRevoked, CodeVersionRollback, CodeVersionJump, CodeBodyTooLarge, CodeBodyMismatch, CodeGroupDenied,
	CodeNotAllowed, CodePinMismatch, CodeTicketMissing, CodeTicketSig, CodeTicketInvalid, CodeTicketHost,
	CodeTicketMismatch, CodeTicketExpired, CodeParamsInvalid, CodeRunIDFormat, CodeRunIDReplay,
	CodeRateInterval, CodeRateDay, CodeRateHour, CodeBusy, CodeStateWrite, CodeExecFailed,
}

// Olay türleri (POST /api/probe/script-result).
const (
	EventAccepted = "accepted"
	EventRejected = "rejected"
	EventReceipt  = "receipt"
	EventNotice   = "notice"
)

// Bildirim kodları (EventNotice).
const (
	NoticeClockForward = "clock_forward"
	NoticeQueueDropped = "queue_dropped"
)

// Makbuz sonuçları.
const (
	StatusOK        = "ok"
	StatusFailed    = "failed"
	StatusTimeout   = "timeout"
	StatusCancelled = "cancelled"
	StatusLost      = "lost"
)

// Onay sınıfları.
const (
	ApprovalNone   = "none"
	ApprovalPanel  = "panel"
	ApprovalSigned = "signed"
)

// Zarf türleri.
const (
	KindManifest = "manifest"
	KindTicket   = "ticket"
	KindRevoke   = "revoke"
	KindReceipt  = "receipt"
)

// Yetenek başlıkları.
const (
	HeaderCapability = "X-Probe-Scripts"
	HeaderAgentKey   = "X-Probe-Script-Key"
)

// Kapalı olma nedenleri (X-Probe-Scripts reason=).
const (
	ReasonMissing  = "missing"
	ReasonDisabled = "disabled"
	ReasonConfig   = "config"
	ReasonState    = "state"
	ReasonPlatform = "platform"
	ReasonInsecure = "insecure"
)
