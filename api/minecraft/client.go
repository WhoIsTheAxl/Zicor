package minecraft

type Client interface {
    SendMessage(message string)
    Name() string
    XUID() string
}

func (c *Client) Name() {

}