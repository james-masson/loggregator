package trafficcontroller

import (
	"code.google.com/p/gogoprotobuf/proto"
	"fmt"
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/loggregatorlib/logmessage"
	"net"
	"net/http"
	"net/url"
	"time"
	"trafficcontroller/authorization"
	"trafficcontroller/hasher"
)

type Proxy struct {
	host      string
	hashers   []*hasher.Hasher
	logger    *gosteno.Logger
	authorize authorization.LogAccessAuthorizer
}

func NewProxy(host string, hashers []*hasher.Hasher, authorizer authorization.LogAccessAuthorizer, logger *gosteno.Logger) *Proxy {
	return &Proxy{host: host, hashers: hashers, authorize: authorizer, logger: logger}
}

func (proxy *Proxy) Start() error {
	return http.ListenAndServe(proxy.host, websocket.Handler(proxy.HandleWebSocket))
}

func (proxy *Proxy) isAuthorized(appId, authToken string, clientAddress net.Addr) (bool, *logmessage.LogMessage) {
	newLogMessage := func(message []byte) *logmessage.LogMessage {
		currentTime := time.Now()
		messageType := logmessage.LogMessage_ERR

		return &logmessage.LogMessage{
			Message:     message,
			AppId:       proto.String(appId),
			MessageType: &messageType,
			SourceName:  proto.String("LGR"),
			Timestamp:   proto.Int64(currentTime.UnixNano()),
		}
	}

	if appId == "" {
		message := fmt.Sprintf("HttpServer: Did not accept sink connection with invalid app id: %s.", clientAddress)
		proxy.logger.Warn(message)
		return false, newLogMessage([]byte("Error: Invalid target"))
	}

	if authToken == "" {
		message := fmt.Sprintf("HttpServer: Did not accept sink connection from %s without authorization.", clientAddress)
		proxy.logger.Warnf(message)
		return false, newLogMessage([]byte("Error: Authorization not provided"))
	}

	if !proxy.authorize(authToken, appId, proxy.logger) {
		message := fmt.Sprintf("HttpServer: Auth token [%s] not authorized to access appId [%s].", authToken, appId)
		proxy.logger.Warn(message)
		return false, newLogMessage([]byte("Error: Invalid authorization"))
	}

	return true, nil
}

func (proxy *Proxy) HandleWebSocket(clientWS *websocket.Conn) {
	req := clientWS.Request()
	req.ParseForm()
	req.Form.Get("app")
	clientAddress := clientWS.RemoteAddr()

	appId := req.Form.Get("app")

	extractAuthTokenFromUrl := func(u *url.URL) string {
		authorization := ""
		queryValues := u.Query()
		if len(queryValues["authorization"]) == 1 {
			authorization = queryValues["authorization"][0]
		}
		return authorization
	}

	authToken := clientWS.Request().Header.Get("Authorization")
	if authToken == "" {
		authToken = extractAuthTokenFromUrl(req.URL)
	}

	if authorized, errorMessage := proxy.isAuthorized(appId, authToken, clientAddress); !authorized {
		data, err := proto.Marshal(errorMessage)
		if err != nil {
			proxy.logger.Errorf("Error marshalling log message: %s", err)
		}
		websocket.Message.Send(clientWS, data)
		clientWS.Close()
		return
	}

	defer clientWS.Close()

	proxy.logger.Debugf("Output Proxy: Request for app: %v", req.Form.Get("app"))
	serverWSs := make([]*websocket.Conn, len(proxy.hashers))
	for index, hasher := range proxy.hashers {
		proxy.logger.Debugf("Output Proxy: Servers in group [%v]: %v", index, hasher.LoggregatorServers())

		server := hasher.GetLoggregatorServerForAppId(appId)
		proxy.logger.Debugf("Output Proxy: AppId is %v. Using server: %v", appId, server)

		config, err := websocket.NewConfig("ws://"+server+req.URL.RequestURI(), "http://localhost")

		if err != nil {
			proxy.logger.Errorf("Output Proxy: Error creating config for websocket - %v", err)
		}

		serverWS, err := websocket.DialConfig(config)
		if err != nil {
			proxy.logger.Errorf("Output Proxy: Error connecting to loggregator server - %v", err)
		}

		if serverWS != nil {
			serverWSs[index] = serverWS
		}
	}
	proxy.forwardIO(serverWSs, clientWS)

}

func (proxy *Proxy) proxyConnectionTo(server *websocket.Conn, client *websocket.Conn, doneChan chan bool) {
	proxy.logger.Debugf("Output Proxy: Starting to listen to server %v", server.RemoteAddr().String())

	var logMessage []byte
	defer server.Close()
	for {
		err := websocket.Message.Receive(server, &logMessage)
		if err != nil {
			proxy.logger.Errorf("Output Proxy: Error reading from the server - %v - %v", err, server.RemoteAddr().String())
			doneChan <- true
			return
		}
		proxy.logger.Debugf("Output Proxy: Got message from server %v bytes", len(logMessage))
		websocket.Message.Send(client, logMessage)
	}
}

func (proxy *Proxy) watchKeepAlive(servers []*websocket.Conn, client *websocket.Conn) {
	var keepAlive []byte
	for {
		err := websocket.Message.Receive(client, &keepAlive)
		if err != nil {
			proxy.logger.Errorf("Output Proxy: Error reading from the client - %v", err)
			return
		}
		proxy.logger.Debugf("Output Proxy: Got message from client %v bytes", len(keepAlive))
		for _, server := range servers {
			websocket.Message.Send(server, keepAlive)
		}
	}
}

func (proxy *Proxy) forwardIO(servers []*websocket.Conn, client *websocket.Conn) {
	doneChan := make(chan bool)

	for _, server := range servers {
		go proxy.proxyConnectionTo(server, client, doneChan)
	}

	go proxy.watchKeepAlive(servers, client)

	for _, server := range servers {
		<-doneChan
		proxy.logger.Debugf("Output Proxy: Lost one server %s", server.RemoteAddr().String())
	}
	proxy.logger.Debugf("Output Proxy: Terminating connection. All clients disconnected")
}
