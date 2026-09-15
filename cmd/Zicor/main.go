package main

import (
    "log"
    "os"
    "sync"
    "io/ioutil"
    "encoding/json"
    "fmt"

    "github.com/pelletier/go-toml"
    "github.com/sandertv/gophertunnel/minecraft"
    "github.com/sandertv/gophertunnel/minecraft/auth"
    "golang.org/x/oauth2"
    "github.com/WhoIsTheAxl/Zicor/internal/packets"
)

func main() {
    config := readConfig()
    
	var token *oauth2.Token
	if data, err := ioutil.ReadFile("token.json"); err == nil {
	    err := json.Unmarshal(data, &token)
	    if err != nil { log.Fatal(err) }
	} else {
		// Placeholder
	}
	if token == nil || !token.Valid() {
	    var err error
	    token, err = auth.RequestLiveToken()
	    if err != nil { panic(err) }
	    data, err := json.Marshal(token)
	    if err != nil { log.Fatal(err) }
	    err = ioutil.WriteFile("token.json", data, 0600)
	    if err != nil { log.Fatal(err) }
	}
	src := auth.RefreshTokenSource(token)
	log.SetPrefix("[ZICOR] ")
	log.Println("Started.")
	
    p, err := minecraft.NewForeignStatusProvider(config.Connection.RemoteAddress)
    if err != nil {
        panic(err)
    }
    log.Println("Status Provider fetched.") 
    listener, err := minecraft.ListenConfig{
        StatusProvider: p,
    }.Listen("raknet", config.Connection.LocalAddress)
    if err != nil {
        panic(err)
    }
    log.Println("Status Provider visible.") 
    log.Printf("Listening on %v.", config.Connection.LocalAddress)
    defer listener.Close()
    
    for {
        c, err := listener.Accept()
		if err != nil {
            panic(err)
        }
        conn := c.(*minecraft.Conn) 
        identity := conn.IdentityData()
		fmt.Printf("Client connected: %s (XUID: %s)\n", identity.DisplayName, identity.XUID)
        go handleConn(c.(*minecraft.Conn), listener, config, src)
    }
}

func handleConn(conn *minecraft.Conn, listener *minecraft.Listener, config config, src oauth2.TokenSource) {
	log.Println("Attempting to connect to %v.", config.Connection.RemoteAddress)
    serverConn, err := minecraft.Dialer{
        TokenSource: src,
        ClientData:  conn.ClientData(),
    }.Dial("raknet", config.Connection.RemoteAddress)
    if err != nil {
        panic(err)
    } else {
    	log.Println("Connection successful")
    }
    
    var g sync.WaitGroup
    g.Add(2)
    go func() {
        if err := conn.StartGame(serverConn.GameData()); err != nil {
            panic(err)
        }
        g.Done()
    }()
    go func() {
        if err := serverConn.DoSpawn(); err != nil {
            panic(err)
        }
        g.Done()
    }()
    g.Wait()

    go func() {
        defer listener.Disconnect(conn, "connection lost")
        defer serverConn.Close()
        for {
		    pk, err := conn.ReadPacket()
		    if err != nil {
		        return
		    }
		    
		    log.Printf("Client -> Server: %T\n", pk)
		    
		    /*switch p := pk.(type) {
		    case *packet.Text:
		        log.Printf("Message: %s\n", p.Message)
		    }*/
		    
		    pk = packets.ClientToServer(conn, pk)
		    
		    if pk != nil {
		    	if err := serverConn.WritePacket(pk); err != nil {
		        	return
		    	}
		    }
		}
    }()
    
    go func() {
        defer serverConn.Close()
        defer listener.Disconnect(conn, "connection lost")
        for {
		    pk, err := serverConn.ReadPacket()
		    if err != nil {
		        return
		    }
		    
		    log.Printf("Server -> Client: %T\n", pk)
		    
		    /*switch p := pk.(type) {
		    case *packet.SetTitle:
		        log.Printf("Title: %s\n", p.Text)
		    }*/
		    
		    // pk = packets.ServerToClient(pk)
		    
		    if err := conn.WritePacket(pk); err != nil {
		        return
		    }
		}
    }()
}

type config struct {
    Connection struct {
        LocalAddress  string
        RemoteAddress string
    }
}

func readConfig() config {
    c := config{}
    if _, err := os.Stat("config.toml"); os.IsNotExist(err) {
        f, err := os.Create("config.toml")
        if err != nil {
            log.Fatalf("create config: %v", err)
        }
        data, err := toml.Marshal(c)
        if err != nil {
            log.Fatalf("encode default config: %v", err)
        }
        if _, err := f.Write(data); err != nil {
            log.Fatalf("write default config: %v", err)
        }
        _ = f.Close()
    }
    data, err := os.ReadFile("config.toml")
    if err != nil {
        log.Fatalf("read config: %v", err)
    }
    if err := toml.Unmarshal(data, &c); err != nil {
        log.Fatalf("decode config: %v", err)
    }
    if c.Connection.LocalAddress == "" {
        c.Connection.LocalAddress = "0.0.0.0:19132"
    }
    data, _ = toml.Marshal(c)
    if err := os.WriteFile("config.toml", data, 0644); err != nil {
        log.Fatalf("write config: %v", err)
    }
    return c
}