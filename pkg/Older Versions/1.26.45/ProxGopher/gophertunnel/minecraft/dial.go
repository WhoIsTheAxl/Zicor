package minecraft

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/df-mc/go-playfab/v2"
	"github.com/df-mc/go-xsapi/v2"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/auth"
	"github.com/sandertv/gophertunnel/minecraft/internal"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/service"
	"golang.org/x/oauth2"
)

var zicorLog = log.New(log.Writer(), "[ZICOR] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)

// Dialer allows specifying specific settings for connection to a Minecraft server.
// The zero value of Dialer is used for the package level Dial function.
type Dialer struct {
	// ErrorLog is a log.Logger that errors that occur during packet handling of
	// servers are written to. By default, errors are not logged.
	ErrorLog *slog.Logger

	// HTTPClient is the HTTP client used for outbound HTTP requests needed by the dialer,
	// such as fetching OpenID configuration/JWKs when authentication is enabled.
	// If nil, [http.DefaultClient] is used.
	HTTPClient *http.Client

	// ClientData is the client data used to login to the server with. It includes fields such as the skin,
	// locale and UUIDs unique to the client. If empty, a default is sent produced using defaultClientData().
	ClientData login.ClientData
	// IdentityData is the identity data used to login to the server with. It includes the username, UUID and
	// XUID of the player.
	// The IdentityData object is obtained using Minecraft auth if Email and Password are set. If not, the
	// object provided here is used, or a default one if left empty.
	IdentityData login.IdentityData

	// TokenSource is the source for Microsoft Live Connect tokens. If set to a non-nil oauth2.TokenSource,
	// this field is used to obtain tokens which in turn are used to authenticate to XBOX Live.
	// The minecraft/auth package provides an oauth2.TokenSource implementation (auth.tokenSource) to use
	// device auth to login.
	// If TokenSource is nil, the connection will not use authentication.
	TokenSource oauth2.TokenSource

	// XBLClient is the Xbox Live API Client used during authenticated login. When
	// set, it is used to request Minecraft authentication chain data and, when
	// [Dialer.EnableLegacyAuth] is false and [Dialer.PlayFabClient] is nil, to
	// log in to PlayFab. If nil, [Dialer.TokenSource] is used directly.
	XBLClient *xsapi.Client

	// PlayFabClient is the PlayFab client used to log in to Minecraft network services and request multiplayer
	// tokens when [Dialer.EnableLegacyAuth] is set to false. To log in to Minecraft network services correctly,
	// it must be authenticated with a PlayFab account in the title ID '20CA2' that has Xbox Live account linked.
	// If nil, one is created from [Dialer.XBLClient] or [Dialer.TokenSource] when required for authenticated login.
	//
	// Setting PlayFabClient alone does not enable authenticated login. The dialer still needs [Dialer.XBLClient]
	// or [Dialer.TokenSource] to request the legacy Minecraft chain used to populate trusted identity data.
	PlayFabClient *playfab.Client

	// PacketFunc is called whenever a packet is read from or written to the connection returned when using
	// Dialer.Dial(). It includes packets that are otherwise covered in the connection sequence, such as the
	// Login packet. The function is called with the header of the packet and its raw payload, the address
	// from which the packet originated, and the destination address.
	PacketFunc func(header packet.Header, payload []byte, src, dst net.Addr)

	// DownloadResourcePack is called individually for every texture and behaviour pack sent by the connection when
	// using Dialer.Dial(), and can be used to stop the pack from being downloaded. The function is called with the UUID
	// and version of the resource pack, the number of the current pack being downloaded, and the total amount of packs.
	// The boolean returned determines if the pack will be downloaded or not.
	DownloadResourcePack func(id uuid.UUID, version string, current, total int) bool
	// ResourcePackCache, if set, reuses resource packs downloaded on earlier logins. Misses and errors
	// fall back to a normal download.
	ResourcePackCache ResourcePackCache

	// DisconnectOnUnknownPackets specifies if the connection should disconnect if packets received are not present
	// in the packet pool. If true, such packets lead to the connection being closed immediately.
	// If set to false, the packets will be returned as a packet.Unknown.
	DisconnectOnUnknownPackets bool

	// DisconnectOnInvalidPackets specifies if invalid packets (either too few bytes or too many bytes) should be
	// allowed. If true, such packets lead to the connection being closed immediately. If false,
	// packets with too many bytes will be returned while packets with too few bytes will be skipped.
	DisconnectOnInvalidPackets bool

	// Protocol is the Protocol version used to communicate with the target server. By default, this field is
	// set to the current protocol as implemented in the minecraft/protocol package. Note that packets written
	// to and read from the Conn are always any of those found in the protocol/packet package, as packets
	// are converted from and to this Protocol.
	Protocol Protocol

	// FlushRate is the rate at which packets sent are flushed. Packets are buffered for a duration up to
	// FlushRate and are compressed/encrypted together to improve compression ratios. The lower this
	// time.Duration, the lower the latency but the less efficient both network and cpu wise.
	// The default FlushRate (when set to 0) is time.Second/20. If FlushRate is set negative, packets
	// will not be flushed automatically. In this case, calling `(*Conn).Flush()` is required after any
	// calls to `(*Conn).Write()` or `(*Conn).WritePacket()` to send the packets over network.
	FlushRate time.Duration

	// EnableClientCache, if set to true, enables the client blob cache for the client. This means that the
	// server will send chunks as blobs, which may be saved by the client so that chunks don't have to be
	// transmitted every time, resulting in less network transmission.
	EnableClientCache bool

	// KeepXBLIdentityData, if set to true, enables passing XUID and title ID to the target server
	// if the authentication token is not set. This is technically not valid and some servers might kick
	// the client when an XUID is present without logging in.
	// For getting this to work with BDS, authentication should be disabled.
	KeepXBLIdentityData bool

	// EnableLegacyAuth, if set to true, will use the legacy authentication behavior
	// (pre-1.21.90) when connecting to the server. This should only be used for outdated
	// servers, as enabling it will cause compatibility issues with updated servers.
	EnableLegacyAuth bool
}

// Dial dials a Minecraft connection to the address passed over the network passed. The network is typically
// "raknet". A Conn is returned which may be used to receive packets from and send packets to.
//
// A zero value of a Dialer struct is used to initiate the connection. A custom Dialer may be used to specify
// additional behaviour.
func Dial(network, address string) (*Conn, error) {
	zicorLog.Printf("Dial called: network=%q address=%q", network, address)
	var d Dialer
	return d.Dial(network, address)
}

// DialTimeout dials a Minecraft connection to the address passed over the network passed. The network is
// typically "raknet". A Conn is returned which may be used to receive packets from and send packets to.
// If a connection is not established before the timeout ends, DialTimeout returns an error.
// DialTimeout uses a zero value of Dialer to initiate the connection.
func DialTimeout(network, address string, timeout time.Duration) (*Conn, error) {
	zicorLog.Printf("DialTimeout called: network=%q address=%q timeout=%s", network, address, timeout)
	var d Dialer
	return d.DialTimeout(network, address, timeout)
}

// DialContext dials a Minecraft connection to the address passed over the network passed. The network is
// typically "raknet". A Conn is returned which may be used to receive packets from and send packets to.
// If a connection is not established before the context passed is cancelled, DialContext returns an error.
// DialContext uses a zero value of Dialer to initiate the connection.
func DialContext(ctx context.Context, network, address string) (*Conn, error) {
	zicorLog.Printf("DialContext called: network=%q address=%q", network, address)
	var d Dialer
	return d.DialContext(ctx, network, address)
}

// Dial dials a Minecraft connection to the address passed over the network passed. The network is typically
// "raknet". A Conn is returned which may be used to receive packets from and send packets to.
func (d Dialer) Dial(network, address string) (*Conn, error) {
	zicorLog.Printf("Dialer.Dial: network=%q address=%q", network, address)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	return d.DialContext(ctx, network, address)
}

// DialTimeout dials a Minecraft connection to the address passed over the network passed. The network is
// typically "raknet". A Conn is returned which may be used to receive packets from and send packets to.
// If a connection is not established before the timeout ends, DialTimeout returns an error.
func (d Dialer) DialTimeout(network, address string, timeout time.Duration) (*Conn, error) {
	zicorLog.Printf("Dialer.DialTimeout: network=%q address=%q timeout=%s", network, address, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return d.DialContext(ctx, network, address)
}

// DialContextNetwork dials a Minecraft connection to the address passed over the Network implementation
// passed. The network is typically [RakNet]. A Conn is returned which may be used to receive packets from
// and send packets to. If a connection is not established before the context passed is cancelled,
// DialContextNetwork returns an error.
func (d Dialer) DialContextNetwork(ctx context.Context, network Network, address string) (conn *Conn, err error) {
	zicorLog.Printf("DialContextNetwork: start network=%T address=%q", network, address)

	if ctx == nil {
		zicorLog.Printf("DialContextNetwork: ctx was nil, using context.Background()")
		ctx = context.Background()
	}
	if d.ErrorLog == nil {
		zicorLog.Printf("DialContextNetwork: ErrorLog was nil, using discard handler")
		d.ErrorLog = slog.New(internal.DiscardHandler{})
	}
	d.ErrorLog = d.ErrorLog.With("src", "dialer")
	if d.Protocol == nil {
		zicorLog.Printf("DialContextNetwork: Protocol was nil, using DefaultProtocol")
		d.Protocol = DefaultProtocol
	}
	if d.FlushRate == 0 {
		zicorLog.Printf("DialContextNetwork: FlushRate was 0, defaulting to %s", time.Second/20)
		d.FlushRate = time.Second / 20
	}
	if d.HTTPClient == nil {
		zicorLog.Printf("DialContextNetwork: HTTPClient was nil, using http.DefaultClient")
		d.HTTPClient = http.DefaultClient
	}

	zicorLog.Printf("DialContextNetwork: generating ECDSA P-384 key")
	key, err := ecdsa.GenerateKey(elliptic.P384(), cryptorand.Reader)
	if err != nil {
		zicorLog.Printf("DialContextNetwork: ECDSA key generation failed: %v", err)
		return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: fmt.Errorf("generating ECDSA key: %w", err)}
	}
	zicorLog.Printf("DialContextNetwork: ECDSA key generated successfully")
	var (
		chainData, token string
		verifier         *oidc.IDTokenVerifier
	)
	if d.PlayFabClient != nil && d.TokenSource == nil && d.XBLClient == nil {
		zicorLog.Printf("DialContextNetwork: PlayFabClient set without XBLClient or TokenSource - rejecting")
		return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: errors.New("PlayFabClient requires XBLClient or TokenSource for authenticated login")}
	}
	if d.TokenSource != nil || d.XBLClient != nil {
		zicorLog.Printf("DialContextNetwork: authenticated login path (TokenSource=%t XBLClient=%t)", d.TokenSource != nil, d.XBLClient != nil)
		ctx = auth.WithContextClient(ctx, d.HTTPClient)
		if d.XBLClient == nil {
			zicorLog.Printf("DialContextNetwork: XBLClient nil, attempting to create from TokenSource")
			x, ok := d.TokenSource.(xsapi.TokenSource)
			if !ok {
				zicorLog.Printf("DialContextNetwork: TokenSource is not xsapi.TokenSource, wrapping with auth.ContextSession")
				x = auth.ContextSession(ctx, d.TokenSource)
			}
			zicorLog.Printf("DialContextNetwork: creating xsapi.Client (RTADisabled)")
			d.XBLClient, err = xsapi.ClientConfig{
				HTTPClient: d.HTTPClient,
				RTAMode:    xsapi.RTADisabled,
			}.New(ctx, x)
			if err != nil {
				zicorLog.Printf("DialContextNetwork: xsapi login failed: %v", err)
				return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: fmt.Errorf("login to xbox live: %w", err)}
			}
			zicorLog.Printf("DialContextNetwork: xsapi.Client created successfully")
			defer d.XBLClient.Close()
		} else {
			zicorLog.Printf("DialContextNetwork: using provided XBLClient")
		}
		if !d.EnableLegacyAuth {
			zicorLog.Printf("DialContextNetwork: non-legacy auth, fetching auth environment")
			e, err := authEnv(ctx)
			if err != nil {
				zicorLog.Printf("DialContextNetwork: authEnv failed: %v", err)
				return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: fmt.Errorf("request authorization environment: %w", err)}
			}
			zicorLog.Printf("DialContextNetwork: auth environment fetched, creating OIDC verifier")
			verifier, err = e.VerifierContext(ctx)
			if err != nil {
				zicorLog.Printf("DialContextNetwork: OIDC verifier creation failed: %v", err)
				return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: fmt.Errorf("create OIDC verifier: %w", err)}
			}
			zicorLog.Printf("DialContextNetwork: OIDC verifier created successfully")

			m, ok := d.TokenSource.(MultiplayerTokenSource)
			if !ok {
				zicorLog.Printf("DialContextNetwork: TokenSource is not MultiplayerTokenSource, using PlayFab path")
				// If a MultiplayerTokenSource was not provided, log in to PlayFab
				// account and use a default implementation instead.
				if d.PlayFabClient == nil {
					zicorLog.Printf("DialContextNetwork: logging into PlayFab with Xbox (titleID=%q)", e.PlayFabTitleID)
					client, err := playfab.LoginWithXbox(ctx, e.PlayFabTitleID, d.XBLClient, playfab.ClientConfig{
						HTTPClient:    d.HTTPClient,
						CreateAccount: true,
					})
					if err != nil {
						zicorLog.Printf("DialContextNetwork: PlayFab login failed: %v", err)
						return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: fmt.Errorf("login to playfab: %w", err)}
					}
					zicorLog.Printf("DialContextNetwork: PlayFab login successful")
					defer client.Close()

					d.PlayFabClient = client
				} else {
					zicorLog.Printf("DialContextNetwork: using provided PlayFabClient")
				}
				m = &multiplayerTokenSource{src: e.TokenSource(d.PlayFabClient, service.TokenConfig{}), env: e}
			} else {
				zicorLog.Printf("DialContextNetwork: using provided MultiplayerTokenSource")
			}
			zicorLog.Printf("DialContextNetwork: requesting multiplayer token")
			token, err = m.MultiplayerToken(ctx, &key.PublicKey)
			if err != nil {
				zicorLog.Printf("DialContextNetwork: MultiplayerToken failed: %v", err)
				return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: err}
			}
			zicorLog.Printf("DialContextNetwork: multiplayer token obtained (len=%d)", len(token))
		} else {
			zicorLog.Printf("DialContextNetwork: legacy auth enabled, skipping multiplayer token")
		}
		zicorLog.Printf("DialContextNetwork: requesting Minecraft auth chain")
		chainData, err = auth.RequestMinecraftChain(ctx, d.XBLClient, key)
		if err != nil {
			zicorLog.Printf("DialContextNetwork: RequestMinecraftChain failed: %v", err)
			return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: fmt.Errorf("request Minecraft auth chain: %w", err)}
		}
		zicorLog.Printf("DialContextNetwork: Minecraft auth chain received (len=%d)", len(chainData))
		identityData, err := readChainIdentityData([]byte(chainData))
		if err != nil {
			zicorLog.Printf("DialContextNetwork: readChainIdentityData failed: %v", err)
			return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: err}
		}
		zicorLog.Printf("DialContextNetwork: chain identity data parsed: Identity=%q DisplayName=%q XUID=%q TitleID=%q",
			identityData.Identity, identityData.DisplayName, identityData.XUID, identityData.TitleID)
		d.IdentityData = identityData
	} else {
		zicorLog.Printf("DialContextNetwork: offline/unauthenticated login path")
	}

	var pong []byte

	zicorLog.Printf("DialContextNetwork: pinging address %q (timeout 3s)", address)
	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)

	pong, err = network.PingContext(pingCtx, address)

	pingCancel()

	if err == nil {
		zicorLog.Printf("DialContextNetwork: ping succeeded, pong len=%d, raw=%q", len(pong), string(pong))
		oldAddress := address
		address = addressWithPongPort(pong, address)
		if oldAddress != address {
			zicorLog.Printf("DialContextNetwork: address redirected via pong port: %q -> %q", oldAddress, address)
		} else {
			zicorLog.Printf("DialContextNetwork: pong did not change address (%q)", address)
		}
	} else {
		zicorLog.Printf("DialContextNetwork: ping failed (continuing anyway): %v", err)
	}

	var netConn net.Conn
	if i, ok := network.(identityDialer); ok && token != "" {
		zicorLog.Printf("DialContextNetwork: using identityDialer.DialContextIdentity")
		netConn, err = i.DialContextIdentity(ctx, address, token, key)
	} else {
		if _, ok := network.(identityDialer); ok {
			zicorLog.Printf("DialContextNetwork: network supports identityDialer but token empty, falling back to DialContext")
		} else {
			zicorLog.Printf("DialContextNetwork: network does not support identityDialer, using DialContext")
		}
		netConn, err = network.DialContext(ctx, address)
	}
	if err != nil {
		zicorLog.Printf("DialContextNetwork: network dial failed: %v", err)
		return nil, err
	}
	zicorLog.Printf("DialContextNetwork: network connection established (local=%v remote=%v)", netConn.LocalAddr(), netConn.RemoteAddr())

	conn = newConn(netConn, key, d.ErrorLog, d.Protocol, d.FlushRate, false)
	conn.pool = conn.proto.Packets(false)
	conn.identityData = d.IdentityData
	conn.clientData = d.ClientData
	conn.packetFunc = d.PacketFunc
	conn.downloadResourcePack = d.DownloadResourcePack
	conn.resourcePackCache = d.ResourcePackCache
	conn.cacheEnabled = d.EnableClientCache
	conn.disconnectOnInvalidPacket = d.DisconnectOnInvalidPackets
	conn.disconnectOnUnknownPacket = d.DisconnectOnUnknownPackets
	conn.maxDecompressedLen = math.MaxInt

	zicorLog.Printf("DialContextNetwork: applying defaultIdentityData/defaultClientData")
	defaultIdentityData(&conn.identityData)
	defaultClientData(address, conn.identityData.DisplayName, &conn.clientData)
	zicorLog.Printf("DialContextNetwork: identity after defaults: Identity=%q DisplayName=%q XUID=%q TitleID=%q",
		conn.identityData.Identity, conn.identityData.DisplayName, conn.identityData.XUID, conn.identityData.TitleID)
	zicorLog.Printf("DialContextNetwork: client data after defaults: ServerAddress=%q ThirdPartyName=%q DeviceOS=%d GameVersion=%q ClientRandomID=%d LanguageCode=%q",
		conn.clientData.ServerAddress, conn.clientData.ThirdPartyName, conn.clientData.DeviceOS, conn.clientData.GameVersion, conn.clientData.ClientRandomID, conn.clientData.LanguageCode)

	var request []byte
	if chainData == "" && token == "" {
		zicorLog.Printf("DialContextNetwork: encoding offline login (chainData/token empty)")
		// We haven't logged into the user's XBL account. We create a login request with only one token
		// holding the identity data set in the Dialer after making sure we clear data from the identity data
		// that is only present when logged in.
		if !d.KeepXBLIdentityData {
			zicorLog.Printf("DialContextNetwork: clearing XBL identity data (KeepXBLIdentityData=false)")
			clearXBLIdentityData(&conn.identityData)
		}
		request = login.EncodeOffline(conn.identityData, conn.clientData, key, d.EnableLegacyAuth)
	} else {
		zicorLog.Printf("DialContextNetwork: encoding authenticated login (chainData len=%d, token len=%d)", len(chainData), len(token))
		// We login as an Android device and this will show up in the 'titleId' field in the JWT chain, which
		// we can't edit. We just enforce Android data for logging in.
		setAndroidData(&conn.clientData)

		request = login.Encode(chainData, conn.clientData, key, token, d.EnableLegacyAuth)
		identityData, _, _, _ := login.Parse(request, verifier)
		// If we got the identity data from Minecraft auth, we need to make sure we set it in the Conn too, as
		// we are not aware of the identity data ourselves yet.
		conn.identityData = identityData
		zicorLog.Printf("DialContextNetwork: parsed identity data from request: Identity=%q DisplayName=%q XUID=%q TitleID=%q",
			identityData.Identity, identityData.DisplayName, identityData.XUID, identityData.TitleID)
	}
	zicorLog.Printf("DialContextNetwork: login request encoded (len=%d, legacyAuth=%t)", len(request), d.EnableLegacyAuth)

	readyForLogin, connected := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancelCause(ctx)
	zicorLog.Printf("DialContextNetwork: starting listenConn goroutine")
	go listenConn(conn, readyForLogin, connected, cancel)

	conn.expect(packet.IDNetworkSettings, packet.IDPlayStatus)
	zicorLog.Printf("DialContextNetwork: sending RequestNetworkSettings (clientProtocol=%d)", d.Protocol.ID())
	if err := conn.WritePacket(&packet.RequestNetworkSettings{ClientProtocol: d.Protocol.ID()}); err != nil {
		zicorLog.Printf("DialContextNetwork: WritePacket(RequestNetworkSettings) failed: %v", err)
		return nil, conn.wrap(fmt.Errorf("send request network settings: %w", err), "dial")
	}
	_ = conn.Flush()
	zicorLog.Printf("DialContextNetwork: RequestNetworkSettings flushed, waiting for readyForLogin")

	select {
	case <-ctx.Done():
		zicorLog.Printf("DialContextNetwork: context done while waiting for network settings: %v", context.Cause(ctx))
		return nil, conn.wrap(context.Cause(ctx), "dial")
	case <-conn.ctx.Done():
		zicorLog.Printf("DialContextNetwork: conn closed while waiting for network settings: %v", conn.closeErr("dial"))
		return nil, conn.closeErr("dial")
	case <-readyForLogin:
		zicorLog.Printf("DialContextNetwork: readyForLogin signal received, sending Login packet")
		// We've received our network settings, so we can now send our login request.
		conn.expect(packet.IDServerToClientHandshake, packet.IDPlayStatus)
		if err := conn.WritePacket(&packet.Login{ConnectionRequest: request, ClientProtocol: d.Protocol.ID()}); err != nil {
			zicorLog.Printf("DialContextNetwork: WritePacket(Login) failed: %v", err)
			return nil, conn.wrap(fmt.Errorf("send login: %w", err), "dial")
		}
		_ = conn.Flush()
		zicorLog.Printf("DialContextNetwork: Login flushed, waiting for connected")

		select {
		case <-ctx.Done():
			zicorLog.Printf("DialContextNetwork: context done while waiting for connected: %v", context.Cause(ctx))
			return nil, conn.wrap(context.Cause(ctx), "dial")
		case <-conn.ctx.Done():
			zicorLog.Printf("DialContextNetwork: conn closed while waiting for connected: %v", conn.closeErr("dial"))
			return nil, conn.closeErr("dial")
		case <-connected:
			zicorLog.Printf("DialContextNetwork: connected signal received, dial successful")
			// We've connected successfully. We return the connection and no error.
			return conn, nil
		}
	}
}

// DialContext dials a Minecraft connection to the address passed over the network passed. The network is
// typically "raknet". A Conn is returned which may be used to receive packets from and send packets to.
// If a connection is not established before the context passed is cancelled, DialContext returns an error.
func (d Dialer) DialContext(ctx context.Context, network, address string) (conn *Conn, err error) {
	zicorLog.Printf("Dialer.DialContext: network=%q address=%q", network, address)
	if d.ErrorLog == nil {
		d.ErrorLog = slog.New(internal.DiscardHandler{})
	}
	d.ErrorLog = d.ErrorLog.With("src", "dialer")
	n, ok := networkByID(network, d.ErrorLog)
	if !ok {
		zicorLog.Printf("Dialer.DialContext: no network under id %q", network)
		return nil, &net.OpError{Op: "dial", Net: "minecraft", Err: fmt.Errorf("dial: no network under id %v", network)}
	}
	zicorLog.Printf("Dialer.DialContext: resolved network %q -> %T", network, n)
	return d.DialContextNetwork(ctx, n, address)
}

// readChainIdentityData reads a login.IdentityData from the Mojang chain
// obtained through authentication.
func readChainIdentityData(chainData []byte) (login.IdentityData, error) {
	zicorLog.Printf("readChainIdentityData: parsing chain data (len=%d)", len(chainData))
	chain := struct{ Chain []string }{}
	if err := json.Unmarshal(chainData, &chain); err != nil {
		zicorLog.Printf("readChainIdentityData: json unmarshal failed: %v", err)
		return login.IdentityData{}, fmt.Errorf("read chain: read json: %w", err)
	}
	zicorLog.Printf("readChainIdentityData: chain has %d entries", len(chain.Chain))
	data := chain.Chain[1]
	claims := struct {
		ExtraData login.IdentityData `json:"extraData"`
	}{}
	tok, err := jwt.ParseSigned(data, []jose.SignatureAlgorithm{jose.ES384})
	if err != nil {
		zicorLog.Printf("readChainIdentityData: jwt parse failed: %v", err)
		return login.IdentityData{}, fmt.Errorf("read chain: parse jwt: %w", err)
	}
	if err := tok.UnsafeClaimsWithoutVerification(&claims); err != nil {
		zicorLog.Printf("readChainIdentityData: unsafe claims failed: %v", err)
		return login.IdentityData{}, fmt.Errorf("read chain: read claims: %w", err)
	}
	if claims.ExtraData.Identity == "" {
		zicorLog.Printf("readChainIdentityData: no extra data found in claims")
		return login.IdentityData{}, fmt.Errorf("read chain: no extra data found")
	}
	zicorLog.Printf("readChainIdentityData: identity data read successfully: Identity=%q DisplayName=%q XUID=%q TitleID=%q",
		claims.ExtraData.Identity, claims.ExtraData.DisplayName, claims.ExtraData.XUID, claims.ExtraData.TitleID)
	return claims.ExtraData, nil
}

// listenConn listens on the connection until it is closed on another goroutine. The channel passed will
// receive a value once the connection is logged in.
func listenConn(conn *Conn, readyForLogin, connected chan struct{}, cancel context.CancelCauseFunc) {
	zicorLog.Printf("listenConn: starting")
	defer func() {
		zicorLog.Printf("listenConn: exiting, closing conn")
		_ = conn.Close()
	}()
	cancelContext := true
	for {
		// We finally arrived at the packet decoding loop. We constantly decode packets that arrive
		// and push them to the Conn so that they may be processed.
		packets, err := conn.dec.Decode()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				if cancelContext {
					zicorLog.Printf("listenConn: decode error, cancelling context: %v", err)
					cancel(err)
				} else {
					zicorLog.Printf("listenConn: decode error after login: %v", err)
					conn.log.Error(err.Error())
				}
			} else {
				zicorLog.Printf("listenConn: decode error net.ErrClosed (expected on shutdown)")
			}
			return
		}
		zicorLog.Printf("listenConn: decoded %d packet(s)", len(packets))
		for _, data := range packets {
			loggedInBefore, readyToLoginBefore := conn.loggedIn, conn.readyToLogin
			if err := conn.receive(data); err != nil {
				if cancelContext {
					zicorLog.Printf("listenConn: receive error, cancelling context: %v", err)
					cancel(err)
				} else {
					zicorLog.Printf("listenConn: receive error after login: %v", err)
					conn.log.Error(err.Error())
				}
				return
			}
			if !readyToLoginBefore && conn.readyToLogin {
				zicorLog.Printf("listenConn: readyToLogin transitioned false->true, signaling readyForLogin")
				// This is the signal that the connection is ready to login, so we put a value in the channel so that
				// it may be detected.
				readyForLogin <- struct{}{}
			}
			if !loggedInBefore && conn.loggedIn {
				zicorLog.Printf("listenConn: loggedIn transitioned false->true, signaling connected")
				// This is the signal that the connection was considered logged in, so we put a value in the channel so
				// that it may be detected.
				cancelContext = false
				connected <- struct{}{}
			}
		}
	}
}

//go:embed skin_resource_patch.json
var skinResourcePatch []byte

//go:embed skin_geometry.json
var skinGeometry []byte

// defaultClientData edits the ClientData passed to have defaults set to all fields that were left unchanged.
func defaultClientData(address, username string, d *login.ClientData) {
	zicorLog.Printf("defaultClientData: start address=%q username=%q", address, username)
	d.ServerAddress = address
	d.ThirdPartyName = username
	if d.DeviceOS == 0 {
		zicorLog.Printf("defaultClientData: DeviceOS was 0, setting to Android")
		d.DeviceOS = protocol.DeviceAndroid
	}
	if d.DefaultInputMode == 0 {
		zicorLog.Printf("defaultClientData: DefaultInputMode was 0, setting to Touch")
		d.DefaultInputMode = packet.InputModeTouch
	}
	if d.CurrentInputMode == 0 {
		zicorLog.Printf("defaultClientData: CurrentInputMode was 0, setting to Touch")
		d.CurrentInputMode = packet.InputModeTouch
	}
	if d.GameVersion == "" {
		zicorLog.Printf("defaultClientData: GameVersion was empty, setting to %q", protocol.CurrentVersion)
		d.GameVersion = protocol.CurrentVersion
	}
	if d.ClientRandomID == 0 {
		d.ClientRandomID = rand.Int63()
		zicorLog.Printf("defaultClientData: ClientRandomID was 0, generated %d", d.ClientRandomID)
	}
	if d.DeviceID == "" {
		d.DeviceID = d.ExpectedDeviceIDFormat().Generate()
		zicorLog.Printf("defaultClientData: DeviceID was empty, generated %q", d.DeviceID)
	}
	if d.LanguageCode == "" {
		zicorLog.Printf("defaultClientData: LanguageCode was empty, setting to en_GB")
		d.LanguageCode = "en_GB"
	}
	if d.PlayFabID == "" {
		id := make([]byte, 8)
		_, _ = cryptorand.Read(id)
		d.PlayFabID = hex.EncodeToString(id)
		zicorLog.Printf("defaultClientData: PlayFabID was empty, generated %q", d.PlayFabID)
	}

	if d.AnimatedImageData == nil {
		zicorLog.Printf("defaultClientData: AnimatedImageData was nil, initializing empty")
		d.AnimatedImageData = make([]login.SkinAnimation, 0)
	}
	if d.PersonaPieces == nil {
		zicorLog.Printf("defaultClientData: PersonaPieces was nil, initializing empty")
		d.PersonaPieces = make([]login.PersonaPiece, 0)
	}
	if d.PieceTintColours == nil {
		zicorLog.Printf("defaultClientData: PieceTintColours was nil, initializing empty")
		d.PieceTintColours = make([]login.PersonaPieceTintColour, 0)
	}
	if d.SelfSignedID == "" {
		d.SelfSignedID = uuid.New().String()
		zicorLog.Printf("defaultClientData: SelfSignedID was empty, generated %q", d.SelfSignedID)
	}
	if d.SkinID == "" {
		d.SkinID = uuid.New().String()
		zicorLog.Printf("defaultClientData: SkinID was empty, generated %q", d.SkinID)
	}
	if d.SkinData == "" {
		d.SkinData = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0, 0, 0, 255}, 32*64))
		d.SkinImageHeight = 32
		d.SkinImageWidth = 64
		zicorLog.Printf("defaultClientData: SkinData was empty, generated default (w=%d h=%d len=%d)",
			d.SkinImageWidth, d.SkinImageHeight, len(d.SkinData))
	}
	if d.SkinResourcePatch == "" {
		d.SkinResourcePatch = base64.StdEncoding.EncodeToString(skinResourcePatch)
		zicorLog.Printf("defaultClientData: SkinResourcePatch was empty, encoded len=%d", len(d.SkinResourcePatch))
	}
	if d.SkinGeometry == "" {
		d.SkinGeometry = base64.StdEncoding.EncodeToString(skinGeometry)
		zicorLog.Printf("defaultClientData: SkinGeometry was empty, encoded len=%d", len(d.SkinGeometry))
	}
	if d.SkinGeometryVersion == "" {
		d.SkinGeometryVersion = base64.StdEncoding.EncodeToString([]byte("0.0.0"))
		zicorLog.Printf("defaultClientData: SkinGeometryVersion was empty, encoded len=%d", len(d.SkinGeometryVersion))
	}
	zicorLog.Printf("defaultClientData: done")
}

// setAndroidData ensures the login.ClientData passed matches settings you would see on an Android device.
func setAndroidData(data *login.ClientData) {
	zicorLog.Printf("setAndroidData: setting DeviceOS=Android GameVersion=%q", protocol.CurrentVersion)
	data.DeviceOS = protocol.DeviceAndroid
	data.GameVersion = protocol.CurrentVersion
}

// clearXBLIdentityData clears data from the login.IdentityData that is only set when a player is logged into
// XBOX Live.
func clearXBLIdentityData(data *login.IdentityData) {
	zicorLog.Printf("clearXBLIdentityData: clearing XUID=%q TitleID=%q", data.XUID, data.TitleID)
	data.XUID = ""
	data.TitleID = ""
}

// defaultIdentityData edits the IdentityData passed to have defaults set to all fields that were left
// unchanged.
func defaultIdentityData(data *login.IdentityData) {
	zicorLog.Printf("defaultIdentityData: start Identity=%q DisplayName=%q", data.Identity, data.DisplayName)
	if data.Identity == "" {
		data.Identity = uuid.New().String()
		zicorLog.Printf("defaultIdentityData: Identity was empty, generated %q", data.Identity)
	}
	if data.DisplayName == "" {
		data.DisplayName = "Steve"
		zicorLog.Printf("defaultIdentityData: DisplayName was empty, set to Steve")
	}
	zicorLog.Printf("defaultIdentityData: done")
}

// splitPong splits the pong data passed by ;, taking into account escaping these.
func splitPong(s string) []string {
	var runes []rune
	var tokens []string
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\\':
			inEscape = true
		case r == ';':
			tokens = append(tokens, string(runes))
			runes = runes[:0]
		case inEscape:
			inEscape = false
			fallthrough
		default:
			runes = append(runes, r)
		}
	}
	result := append(tokens, string(runes))
	zicorLog.Printf("splitPong: input=%q tokens=%d", s, len(result))
	return result
}

// addressWithPongPort parses the redirect IPv4 port from the pong and returns the address passed with the port
// found if present, or the original address if not.
func addressWithPongPort(pong []byte, address string) string {
	zicorLog.Printf("addressWithPongPort: pong=%q address=%q", string(pong), address)
	frag := splitPong(string(pong))
	if len(frag) > 10 {
		portStr := frag[10]
		port, err := strconv.Atoi(portStr)
		// Vanilla (realms, in particular) will sometimes send port 19132 when you ping a port that isn't 19132 already,
		// but we should ignore that.
		if err != nil || port == 19132 {
			zicorLog.Printf("addressWithPongPort: portStr=%q err=%v port=%d, ignoring redirect", portStr, err, port)
			return address
		}
		// Remove the port from the address.
		addressParts := strings.Split(address, ":")
		address = strings.Join(strings.Split(address, ":")[:len(addressParts)-1], ":")
		result := address + ":" + portStr
		zicorLog.Printf("addressWithPongPort: redirecting to %q", result)
		return result
	}
	zicorLog.Printf("addressWithPongPort: not enough pong fragments (%d), returning original", len(frag))
	return address
}