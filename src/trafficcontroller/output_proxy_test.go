package trafficcontroller

import (
	"github.com/cloudfoundry/loggregatorlib/loggertesthelper"
	messagetesthelpers "github.com/cloudfoundry/loggregatorlib/logmessage/testhelpers"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/url"
	"testing"
	"time"
	"trafficcontroller/hasher"
	testhelpers "trafficcontroller_testhelpers"
)

func TestProxying(t *testing.T) {
	go Server("localhost:62023", "Hello World from the server", 1)

	proxy := NewProxy(
		"localhost:62022",
		[]*hasher.Hasher{hasher.NewHasher([]string{"localhost:62023"})},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	WaitForServerStart("62023", "/")

	receivedChan := Client(t, "62022", "/?app=myApp")

	select {
	case data := <-receivedChan:
		assert.Equal(t, string(data), "Hello World from the server")
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}
}

func TestProxyingWithAuthThroughQueryParam(t *testing.T) {
	go Server("localhost:62038", "Hello World from the server", 1)
	proxy := NewProxy(
		"localhost:62022",
		[]*hasher.Hasher{hasher.NewHasher([]string{"localhost:62038"})},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	WaitForServerStart("62038", "/")

	receivedChan := ClientWithQueryParamAuth(t, "62022", "/?app=myApp")

	select {
	case data := <-receivedChan:
		assert.Equal(t, string(data), "Hello World from the server")
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}
}

func TestProxyingWithTwoMessages(t *testing.T) {
	go Server("localhost:62020", "Hello World from the server", 2)

	proxy := NewProxy(
		"localhost:62021",
		[]*hasher.Hasher{hasher.NewHasher([]string{"localhost:62020"})},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	WaitForServerStart("62020", "/")

	receivedChan := Client(t, "62021", "/?app=myApp")

	messages := ""
	for message := range receivedChan {
		messages = messages + string(message)
	}
	assert.Contains(t, messages, "Hello World from the serverHello World from the server")
}

func TestProxyingWithHashingBetweenServers(t *testing.T) {
	go Server("localhost:62024", "Hello World from the server 1", 1)
	go Server("localhost:62025", "Hello World from the server 2", 1)

	proxy := NewProxy(
		"localhost:62026",
		[]*hasher.Hasher{hasher.NewHasher([]string{"localhost:62024", "localhost:62025"})},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	WaitForServerStart("62024", "/")

	receivedChan := Client(t, "62026", "/?app=0")

	select {
	case data := <-receivedChan:
		assert.Equal(t, string(data), "Hello World from the server 1")
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}

	receivedChan = Client(t, "62026", "/?app=1")

	select {
	case data := <-receivedChan:
		assert.Equal(t, string(data), "Hello World from the server 2")
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}
}

func TestProxyingWithMultipleAZs(t *testing.T) {
	go Server("localhost:62027", "Hello World from the server 1 - AZ1", 1)
	go Server("localhost:62028", "Hello World from the server 2 - AZ1", 1)
	go Server("localhost:62029", "Hello World from the server 1 - AZ2", 1)
	go Server("localhost:62030", "Hello World from the server 2 - AZ2", 1)

	proxy := NewProxy(
		"localhost:62031",
		[]*hasher.Hasher{
			hasher.NewHasher([]string{"localhost:62027", "localhost:62028"}),
			hasher.NewHasher([]string{"localhost:62029", "localhost:62030"}),
		},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	WaitForServerStart("62027", "/")

	receivedChan := Client(t, "62031", "/?app=0")
	messages := ""
	for message := range receivedChan {
		messages = messages + string(message)
	}
	assert.Contains(t, messages, "Hello World from the server 1 - AZ1")
	assert.Contains(t, messages, "Hello World from the server 1 - AZ2")

	receivedChan = Client(t, "62031", "/?app=1")
	messages = ""
	for message := range receivedChan {
		messages = messages + string(message)
	}
	assert.Contains(t, messages, "Hello World from the server 2 - AZ1")
	assert.Contains(t, messages, "Hello World from the server 2 - AZ2")
}

func TestKeepAliveWithMultipleAZs(t *testing.T) {
	keepAliveChan1 := make(chan []byte)
	keepAliveChan2 := make(chan []byte)
	go KeepAliveServer("localhost:62032", keepAliveChan1)
	go KeepAliveServer("localhost:62033", keepAliveChan2)

	proxy := NewProxy(
		"localhost:62034",
		[]*hasher.Hasher{
			hasher.NewHasher([]string{"localhost:62032"}),
			hasher.NewHasher([]string{"localhost:62033"}),
		},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	WaitForServerStart("62032", "/")

	KeepAliveClient(t, "62034", "/?app=myServerApp1")

	select {
	case data := <-keepAliveChan1:
		assert.Equal(t, string(data), "keep alive")
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}
	select {
	case data := <-keepAliveChan2:
		assert.Equal(t, string(data), "keep alive")
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}
}

func TestProxyWhenLogTargetisinvalid(t *testing.T) {
	proxy := NewProxy(
		"localhost:62060",
		[]*hasher.Hasher{
			hasher.NewHasher([]string{"localhost:62032"}),
		},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	time.Sleep(time.Millisecond * 50)

	receivedChan := Client(t, "62060", "/invalid_target")

	select {
	case data := <-receivedChan:
		messagetesthelpers.AssertProtoBufferMessageEquals(t, "Error: Invalid target", data)
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}

	_, stillOpen := <-receivedChan
	assert.False(t, stillOpen)
}

func TestProxyWithoutAuthorization(t *testing.T) {
	proxy := NewProxy(
		"localhost:62061",
		[]*hasher.Hasher{
			hasher.NewHasher([]string{"localhost:62032"}),
		},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	time.Sleep(time.Millisecond * 50)

	ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:62061/?app=myApp", http.Header{})
	assert.NoError(t, err)

	receivedChan := ClientWithAuth(t, ws)

	select {
	case data := <-receivedChan:
		messagetesthelpers.AssertProtoBufferMessageEquals(t, "Error: Authorization not provided", data)
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}

	_, stillOpen := <-receivedChan
	assert.False(t, stillOpen)
}

func TestProxyWhenAuthorizationFailsThroughHeader(t *testing.T) {
	proxy := NewProxy(
		"localhost:62062",
		[]*hasher.Hasher{
			hasher.NewHasher([]string{"localhost:62032"}),
		},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	time.Sleep(time.Millisecond * 50)

	ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:62061/?app=myApp", http.Header{"Authorization": []string{testhelpers.INVALID_AUTHENTICATION_TOKEN}})

	assert.NoError(t, err)

	receivedChan := ClientWithAuth(t, ws)

	select {
	case data := <-receivedChan:
		messagetesthelpers.AssertProtoBufferMessageEquals(t, "Error: Invalid authorization", data)
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}

	_, stillOpen := <-receivedChan
	assert.False(t, stillOpen)
}

func TestProxyWhenAuthorizationFailsThroughQueryParams(t *testing.T) {
	proxy := NewProxy(
		"localhost:62062",
		[]*hasher.Hasher{
			hasher.NewHasher([]string{"localhost:62032"}),
		},
		testhelpers.SuccessfulAuthorizer,
		loggertesthelper.Logger(),
	)
	go proxy.Start()
	time.Sleep(time.Millisecond * 50)

	ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:62061/?app=myApp&authorization="+url.QueryEscape(testhelpers.INVALID_AUTHENTICATION_TOKEN), http.Header{})
	assert.NoError(t, err)
	receivedChan := ClientWithAuth(t, ws)

	select {
	case data := <-receivedChan:
		messagetesthelpers.AssertProtoBufferMessageEquals(t, "Error: Invalid authorization", data)
	case <-time.After(1 * time.Second):
		t.Error("Did not receive response within one second")
	}

	_, stillOpen := <-receivedChan
	assert.False(t, stillOpen)
}

func WaitForServerStart(port string, path string) {
	serverStarted := func() bool {
		_, _, err := websocket.DefaultDialer.Dial("ws://localhost:"+port+path, http.Header{})
		if err != nil {
			return false
		}
		return true
	}
	for !serverStarted() {
		time.Sleep(1 * time.Microsecond)
	}
}

func ClientWithQueryParamAuth(t *testing.T, port string, path string) chan []byte {
	ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:"+port+path+"&authorization="+url.QueryEscape(testhelpers.VALID_AUTHENTICATION_TOKEN), http.Header{})
	assert.NoError(t, err)

	return ClientWithAuth(t, ws)
}

func Client(t *testing.T, port string, path string) chan []byte {
	ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:"+port+path, http.Header{"Authorization": []string{testhelpers.VALID_AUTHENTICATION_TOKEN}})
	assert.NoError(t, err)

	return ClientWithAuth(t, ws)
}

func ClientWithAuth(t *testing.T, ws *websocket.Conn) chan []byte {
	receivedChan := make(chan []byte)

	go func() {
		for {

			_, data, err := ws.ReadMessage()

			if err != nil {
				close(receivedChan)
				ws.Close()
				return
			}
			receivedChan <- data
		}

	}()
	return receivedChan
}

type fakeServer struct {
	response         string
	numberOfMessages int
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if err != nil {
		http.Error(w, "Not a websocket handshake", 400)
		return
	}

	doneChan := make(chan bool)
	defer ws.Close()
	go func() {
		for ii := 1; ii <= f.numberOfMessages; ii++ {
			time.Sleep(time.Duration(ii) * time.Millisecond)

			ws.WriteMessage(websocket.BinaryMessage, []byte(f.response))

			if ii == f.numberOfMessages {
				doneChan <- true
			}

		}
	}()
	<-doneChan
}

func Server(apiEndpoint, response string, numberOfMessages int) {
	fake := fakeServer{response, numberOfMessages}
	err := http.ListenAndServe(apiEndpoint, &fake)
	if err != nil {
		panic(err)
	}
}

type fakeKeepAliveServer struct {
	keepAliveChan chan []byte
}

func (f *fakeKeepAliveServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if err != nil {
		http.Error(w, "Not a websocket handshake", 400)
		return
	}

	doneChan := make(chan bool)
	go func() {
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				doneChan <- true
				return
			}
			f.keepAliveChan <- data

		}
	}()
	<-doneChan
}

func KeepAliveServer(apiEndpoint string, keepAliveChan chan []byte) {
	fakeServer := fakeKeepAliveServer{keepAliveChan}
	err := http.ListenAndServe(apiEndpoint, &fakeServer)
	if err != nil {
		panic(err)
	}
}

func KeepAliveClient(t *testing.T, port string, path string) {
	ws, _, err := websocket.DefaultDialer.Dial("ws://localhost:"+port+path, http.Header{"Authorization": []string{testhelpers.VALID_AUTHENTICATION_TOKEN}})
	assert.NoError(t, err)
	ws.WriteMessage(websocket.BinaryMessage, []byte("keep alive"))
}
