package cProtocol

//Correct head

import (
	"errors"
)

type Direction byte
type Response byte
type ZipType byte
type ContentType byte

const (
	REP Direction = iota
	REQ
)

const (
	NoResponse Response = iota
	HaveResponse
)

type Low struct {
	low   byte
	req   Direction
	rsp   Response
	_type uint8
}

func (p *Low) SetDirection(req Direction) {
	p.req = req
	p.reset()
}

func (p *Low) GetDirection() Direction {
	return p.req
}

func (p *Low) SetResponse(rsp Response) {
	p.rsp = rsp
	p.reset()
}

func (p *Low) GetResponse() Response {
	return p.rsp
}

// SetPriCommand 设置主要命令类型 (0-31)
func (p *Low) SetCommandType(_type uint8) {
	if _type > 31 {
		return
	}
	p._type = _type << 3 >> 3
	p.reset()
}

// GetCommandType 获取主要命令类型
func (p *Low) GetCommandType() uint8 {
	return p._type
}

func (p *Low) LowByte() byte {
	return p.low
}

func (p *Low) reset() {
	p.low = 0
	p.low = p.low | (byte(p.req) << 7)
	p.low = p.low | (byte(p.rsp) << 6)
	p.low = p.low | (1 << 5)
	p.low = p.low | byte(p._type<<3>>3)
}

func (p *Low) SetLow(low byte) {
	p.low = low
	p.req = Direction(p.low >> 7)
	p.rsp = Response(p.low << 1 >> 7)
	p._type = uint8(p.low & 0x1F) // 提取低5位作为命令，保持 uint8 类型
}

// SetConfig req:0,1,rsp:0,1,et:0-31
func (p *Low) SetConfig(req Direction, rsp Response, _type uint8) error {
	if req > 1 || rsp > 1 {
		return errors.New("设置超出监听范围")
	}
	if _type > 31 {
		return errors.New("命令类型超出监听范围")
	}
	p.SetDirection(req)
	p.SetResponse(rsp)
	p.SetCommandType(_type)
	p.reset()
	return nil
}

type HEAD struct {
	Low
	high       byte
	dataLength [4]byte
}

func NewHead() *HEAD {
	return new(HEAD)
}

func HeadFromBytes(protocol []byte) (*HEAD, error) {
	p := new(HEAD)
	p.high = protocol[0]
	p.SetLow(protocol[1])
	copy(p.dataLength[:], protocol[2:6])
	// 验证 high 字节的最低位是否为1（协议版本标识）
	// 验证 low 字节的第5位是否为1（固定标识位）
	if p.high&1 != 1 || p.low&(1<<5) == 0 {
		return nil, errors.New("协议格式有误")
	}
	return p, nil
}

// SetCommand 设置通信命令 (0-127)
func (p *HEAD) SetCommand(command uint8) error {
	if command > 127 {
		return errors.New("命令编码超出监听范围")
	}
	p.high = 1 | command<<1
	return nil
}

// GetCommand 获取通信命令
// 返回值范围: 0-127
func (p *HEAD) GetCommand() byte {
	return p.high >> 1
}

func (p *HEAD) SetContentLength(length uint32) {
	p.dataLength[3] = byte(length << 24 >> 24)
	p.dataLength[2] = byte(length << 16 >> 24)
	p.dataLength[1] = byte(length << 8 >> 24)
	p.dataLength[0] = byte(length >> 24)
}

func (p *HEAD) GetContentLength() uint32 {
	return uint32(p.dataLength[0])<<24 | uint32(p.dataLength[1])<<16 | uint32(p.dataLength[2])<<8 | uint32(p.dataLength[3])
}

func (p *HEAD) GetBytes() []byte {
	bytes := make([]byte, 6)
	bytes[0] = p.high
	p.reset()
	bytes[1] = p.low
	copy(bytes[2:], p.dataLength[:])
	return bytes
}
