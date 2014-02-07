package sinkserver

import (
	messagetesthelpers "github.com/cloudfoundry/loggregatorlib/logmessage/testhelpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	testhelpers "server_testhelpers"
	"testing"
	"time"
	"github.com/cloudfoundry/loggregatorlib/logmessage"
	"github.com/cloudfoundry/loggregatorlib/loggertesthelper"
)

var _ = Describe("LogMessageUnmarshaller", func() {

		var dataReadChannel chan *logmessage.Message

		BeforeEach(func() {
			dataReadChannel = make(chan *logmessage.Message)
			logger := loggertesthelper.Logger()
			sinkManager = NewSinkManager(1024, false, nil, logger)
			go sinkManager.Start()
			TestMessageRouter = NewMessageRouter(dataReadChannel, sinkManager, logger)
		})

		It("should dump all messages to an app user", func(done Done) {
				expectedMessageString := "Some data"
				message := messagetesthelpers.NewMessage(t, expectedMessageString, "myOtherApp")

				dataReadChannel <- message
				dataReadChannel <- message
		})


	})

func dumpAllMessages(receivedChan chan []byte) [][]byte {
	logMessages := [][]byte{}
	for message := range receivedChan {
		logMessages = append(logMessages, message)
	}
	return logMessages
}

func TestItDumpsAllMessagesForAnAppUser(t *testing.T) {
	expectedMessageString := "Some data"
	message := messagetesthelpers.NewMessage(t, expectedMessageString, "myOtherApp")

	dataReadChannel <- message
	dataReadChannel <- message

	receivedChan := make(chan []byte, 2)
	_, stopKeepAlive, droppedChannel := testhelpers.AddWSSink(t, receivedChan, SERVER_PORT, RECENT_LOGS_PATH+"?app=myOtherApp")

	select {
	case <-droppedChannel:
		// we should have been dropped
	case <-time.After(10 * time.Millisecond):
		t.Error("we should have been dropped")
	}

	logMessages := dumpAllMessages(receivedChan)

	assert.Equal(t, len(logMessages), 2)
	messagetesthelpers.AssertProtoBufferMessageEquals(t, expectedMessageString, logMessages[len(logMessages)-1])

	stopKeepAlive <- true
}

func TestItDoesntHangWhenThereAreNoMessages(t *testing.T) {
	receivedChan := make(chan []byte, 1)
	testhelpers.AddWSSink(t, receivedChan, SERVER_PORT, RECENT_LOGS_PATH+"?app=myOtherApp")

	doneChan := make(chan bool)
	go func() {
		dumpAllMessages(receivedChan)
		close(doneChan)
	}()
	select {
	case <-doneChan:
		break
	case <-time.After(10 * time.Millisecond):
		t.Error("Should have returned by now")
	}
}

func TestDumpDropSinkWhenLogTargetisinvalid(t *testing.T) {
	AssertConnectionFails(t, SERVER_PORT, RECENT_LOGS_PATH+"?something=invalidtarget", 4000)
}
