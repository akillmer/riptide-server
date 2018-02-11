# go-socket
A simple way to manage many WebSocket clients

With this package each connected client recieves a unique short ID. It is possible to `Send` or `Broadcast` to all the connected clients. There is no concepts of _rooms_ or _lobbies_ with this package.

All messages received must conform as a `Message` structure:

```go
type Message struct {
	From    string      `json:"-"`
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}
```
`Message.From` is assigned by the package to identify who the message is from, it is never shared with clients. `Message.Type` is assigned by you so you can differentiate message types. `Message.Payload` is the actual data payload for the message.

I wrote this package to help facilitate some projects I'm working on. They are primarily used to communicate a React client (browser) with the server (golang).

Here's a quick example of working with received messages.

```go
msg := <-socket.Read()
switch msg.Type {
    case "ADD_TO_QUEUE":
        if id, ok := msg.Payload.(string); ok {
            // do something with id
        } else {
            // let the client know their request was bad
            socket.Send(msg.From, "ERROR", "you did not provide a valid id!")
        }
}
```

The benefit of having `Payload` being `interface{}` is that your client can push anything, including JSON, and you can test for it on the fly.

```go
if data, ok := msg.Payload.(map[string]interface{}); ok {
    if name, ok := data["name"].(string); ok {
        // now we know the client's name
    }
    if age, ok := data["age"].(int); ok {
        // and their age
    }
}
```

As my other personal projects grow with complexity this package might do the same. I definitely see an advantage of having lobbies/rooms/hubs/etc, as a way to partition clients into groups. But until then...