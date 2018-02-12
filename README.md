# riptide server
`riptide` manages torrents with the [anacrolix/torrent](https://github.com/anacrolix/torrent) package. It exposes a WebSocket service for the frontend and can act as a static file server for the [frontend app](https://github.com/akillmer/riptide-app).

## Usage
Be sure to `go get github.com/akillmer/riptide-server` and then simply `go build`. Personally, I just keep the binary where my heart is.

`riptide -h` gives you a good idea of what options are available to you. If you pass `-devmode` then `riptide` allows any origin to connect to port 9800. It does not serve any static files while in this mode.

After `riptide` has bootstrapped it sends all of its logging to `./riptide.log`.

## Supported features
+ *Magnet URIs* - add torrents by pasting a magnet URI with the frontend. 
+ *Queue manager* - limit the number of active torrents with `riptide`, or force a queued torrent to start through the frontend.
+ *Labels* - assign labels to any torrent at any time. Torrents can be moved to a specific location after being downloaded with labels.
+ *Persistence* - `riptide` stores its state in a `BoltDB` file, so you can stop and go as you please between sessions.
+ *Concurrency* - multiple clients can connect to the same `riptide` server through the frontend app. Check out [akillmer/go-socket](https://github.com/akillmer/go-socket) for more.

## Beware that
+ Torrent files are not supported, only magnet URIs.
+ There's no user auth, anyone on your network can control `riptide`.
+ Symlinks are used when seeding finished torrents after their data has been moved by a label.
+ This project is for fun and not profit, YMMV.

## To be implemented
+ User authorization
+ Smarter labeling
  + Setting seed ratios that supercede the global ratio.
  + Using regular expressions to further organize downloaded torrents.

Be sure to check out my frontend React app over at [akillmer/riptide-client](https://github.com/akillmer/riptide-client).
