package maxminddb

type nodeReader interface {
	readLeft(uint) uint
	readRight(uint) uint
}

type nodeReader24 struct {
	buffer []byte
}

func (n nodeReader24) readLeft(nodeNumber uint) uint {
	return (uint(n.buffer[nodeNumber]) << 16) | (uint(n.buffer[nodeNumber+1]) << 8) | uint(n.buffer[nodeNumber+2])
}

func (n nodeReader24) readRight(nodeNumber uint) uint {
	return (uint(n.buffer[nodeNumber+3]) << 16) | (uint(n.buffer[nodeNumber+4]) << 8) | uint(n.buffer[nodeNumber+5])
}

type nodeReader28 struct {
	buffer []byte
}

func (n nodeReader28) readLeft(nodeNumber uint) uint {
	return ((uint(n.buffer[nodeNumber+3]) & 0xF0) << 20) | (uint(n.buffer[nodeNumber]) << 16) | (uint(n.buffer[nodeNumber+1]) << 8) | uint(n.buffer[nodeNumber+2])
}

func (n nodeReader28) readRight(nodeNumber uint) uint {
	return ((uint(n.buffer[nodeNumber+3]) & 0x0F) << 24) | (uint(n.buffer[nodeNumber+4]) << 16) | (uint(n.buffer[nodeNumber+5]) << 8) | uint(n.buffer[nodeNumber+6])
}

type nodeReader32 struct {
	buffer []byte
}

func (n nodeReader32) readLeft(nodeNumber uint) uint {
	return (uint(n.buffer[nodeNumber]) << 24) | (uint(n.buffer[nodeNumber+1]) << 16) | (uint(n.buffer[nodeNumber+2]) << 8) | uint(n.buffer[nodeNumber+3])
}

func (n nodeReader32) readRight(nodeNumber uint) uint {
	return (uint(n.buffer[nodeNumber+4]) << 24) | (uint(n.buffer[nodeNumber+5]) << 16) | (uint(n.buffer[nodeNumber+6]) << 8) | uint(n.buffer[nodeNumber+7])
}
