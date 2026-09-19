package helpers

import (
    "github.com/sandertv/gophertunnel/minecraft/protocol/packet"
    "github.com/sandertv/gophertunnel/minecraft"
)

func SendZicorMessage(conn *minecraft.Conn, message string) {
    conn.WritePacket(&packet.Text{
        TextType: packet.TextTypeRaw,
        Message:  "[ZICOR] " + message,
    })
}