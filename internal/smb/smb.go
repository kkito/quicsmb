// Package smb 提供最小 SMB2 头嗅探：payload 保持不透明，仅解析方言+命令用于日志。
// 对应 C 版 src/smb/smb.c（MS-SMB2 2.2.1）。
package smb

import "encoding/binary"

// Commands
const (
	CmdNegotiate      = 0x0000
	CmdSessionSetup   = 0x0001
	CmdLogoff         = 0x0002
	CmdTreeConnect    = 0x0003
	CmdTreeDisconnect = 0x0004
	CmdCreate         = 0x0005
	CmdClose          = 0x0006
	CmdFlush          = 0x0007
	CmdRead           = 0x0008
	CmdWrite          = 0x0009
	CmdLock           = 0x000A
	CmdIOCtl          = 0x000B
	CmdCancel         = 0x000C
	CmdEcho           = 0x000D
	CmdQueryDirectory = 0x000E
	CmdChangeNotify   = 0x000F
	CmdQueryInfo      = 0x0010
	CmdSetInfo        = 0x0011
	CmdOplockBreak    = 0x0012
)

// Info 是一次嗅探结果。
type Info struct {
	Dialect uint16
	Cmd     uint16
	MsgLen  uint32
	Valid   bool
}

// Sniff 解析一个 NetBIOS Session Service + SMB2 头，成功返回 Info{Valid:true}。
func Sniff(buf []byte) (Info, bool) {
	var out Info
	if len(buf) < 4+64 {
		return out, false
	}
	out.MsgLen = uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
	h := buf[4:]
	if !(h[0] == 0xFE && h[1] == 'S' && h[2] == 'M' && h[3] == 'B') {
		return out, false
	}
	out.Cmd = binary.LittleEndian.Uint16(h[12:14])
	if len(buf) >= 4+70 {
		out.Dialect = binary.LittleEndian.Uint16(buf[4+64+2 : 4+64+4])
	}
	out.Valid = true
	return out, true
}

// CommandName 返回 SMB2 命令名。
func CommandName(cmd uint16) string {
	switch cmd {
	case CmdNegotiate:
		return "NEGOTIATE"
	case CmdSessionSetup:
		return "SESSION_SETUP"
	case CmdLogoff:
		return "LOGOFF"
	case CmdTreeConnect:
		return "TREE_CONNECT"
	case CmdTreeDisconnect:
		return "TREE_DISCONNECT"
	case CmdCreate:
		return "CREATE"
	case CmdClose:
		return "CLOSE"
	case CmdFlush:
		return "FLUSH"
	case CmdRead:
		return "READ"
	case CmdWrite:
		return "WRITE"
	case CmdLock:
		return "LOCK"
	case CmdIOCtl:
		return "IOCTL"
	case CmdCancel:
		return "CANCEL"
	case CmdEcho:
		return "ECHO"
	case CmdQueryDirectory:
		return "QUERY_DIRECTORY"
	case CmdChangeNotify:
		return "CHANGE_NOTIFY"
	case CmdQueryInfo:
		return "QUERY_INFO"
	case CmdSetInfo:
		return "SET_INFO"
	case CmdOplockBreak:
		return "OPLOCK_BREAK"
	default:
		return "UNKNOWN"
	}
}

// DialectName 返回方言名。
func DialectName(d uint16) string {
	switch d {
	case 0x0202:
		return "SMB 2.0.2"
	case 0x0210:
		return "SMB 2.1"
	case 0x0300:
		return "SMB 3.0"
	case 0x0302:
		return "SMB 3.0.2"
	case 0x0311:
		return "SMB 3.1.1"
	default:
		return "SMB2/3"
	}
}
