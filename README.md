# timebased_0cl

A terminal tool for detecting and exploiting **0.CL HTTP request-smuggling (desync)**
using timing signals, with an interactive TUI for driving and inspecting attacks.

A 0.CL desync happens when the front-end ignores a `Content-Length` header that the
back-end processes. Such cases are hard to confirm with plain differential responses,
so this tool leans on response timing to decide whether the back-end is waiting for a
body that the front-end never forwarded.

> Built as a focused companion to broader request-smuggling tooling, when 0.CL
> exploitation needed its own interactive workflow.

## Install

```
git clone https://github.com/energibeck-source/timebased_0cl
cd timebased_0cl
go build -o timebased_0cl .
```

Requires Go 1.25+.

## Usage

```
./timebased_0cl -u https://target.example -p 443
./timebased_0cl -u https://target.example -req request.txt
```

| Flag    | Description                                         |
|---------|-----------------------------------------------------|
| `-u`    | Target URL                                          |
| `-p`    | Port                                                |
| `-req`  | Optional text file with an HTTP/1.1 request to seed |
| `-help` | Show help                                           |

Controls and live results are shown in the TUI once it starts.

## Legal

For authorized testing only. Use it against systems you own or are explicitly
permitted to test (e.g. a bug-bounty program's in-scope assets). You are
responsible for how you use it.

## License

MIT — see [LICENSE](LICENSE).
