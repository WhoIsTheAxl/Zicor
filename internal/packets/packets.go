package packets

import (
    "github.com/sandertv/gophertunnel/minecraft/protocol/packet"
    "github.com/sandertv/gophertunnel/minecraft"
    //"github.com/WhoIsTheAxl/Zicor/internal/helpers"
)

// mpk = Modified Packet
func ClientToServer(conn *minecraft.Conn, pk packet.Packet) packet.Packet {
    switch mpk := pk.(type) {
    default:
        return mpk
    }
}

func ServerToClient(conn *minecraft.Conn, pk packet.Packet) packet.Packet {
    switch mpk := pk.(type) {
    default:
        return mpk
    }
}