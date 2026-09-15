package packets

import (
    "github.com/sandertv/gophertunnel/minecraft/protocol/packet"
    "github.com/sandertv/gophertunnel/minecraft"
)

// mpk = Modified Packet
func ClientToServer(conn *minecraft.Conn, pk packet.Packet) packet.Packet {
	switch mpk := pk.(type) {
	case *packet.Text:
	    conn.WritePacket(&packet.Text{
	        TextType: packet.TextTypeRaw,
	        Message:  "[ZICOR] Did it send bro?",
	    })
	    return nil
	default:
		return mpk 
	}
}

func ServerToClient(pk packet.Packet) {
	// Placeholder
}