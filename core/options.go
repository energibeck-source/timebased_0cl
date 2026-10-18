package core

import (
  "fmt"
  "flag"
  "log"
  "net/url"
  "bufio"
  "bytes"
  "os"
  "strconv"
)


func ParseOptions() Target {
    var opts Options

    flag.Usage = printUsage
    help := flag.Bool("help", false, "Show this help")

    flag.StringVar(&opts.RawUrl, "u", "", "Target URL")
    flag.IntVar(&opts.Port, "p", 0, "port")
    flag.StringVar(&opts.RequestFile, "req", "", "optional, a text file of request to start with")
    
    flag.Parse()
    
    if *help {
        flag.Usage()
        os.Exit(0)
    }

    if opts.RawUrl != "" {
        url, err := url.Parse(opts.RawUrl)
        if err != nil {
            log.Fatal("Incorrect url: ", err)
        } else if url.Scheme != "http" && url.Scheme != "https" {
            log.Fatal("Incorrect url scheme: ", url.Scheme)
        }
        opts.URL = *url
        url.RawPath = opts.RawUrl
            
    } else {
        log.Fatal("-u <url> or -req <request> with -proto <request-proto> are required")
    }
    
    return initTgtFromOpts(opts)
}

func initTgtFromOpts(opts Options) Target {

    var (
        tls bool
        cr []byte
        port int
    )

    if opts.RawUrl != "" {
        url, err := url.Parse(opts.RawUrl)
        if err != nil {
            log.Fatal("Incorrect url: ", err)
        }
        url.RawPath = opts.RawUrl

        if url.Scheme == "https" {
            tls = true
            port = 443
        } else {
            tls = false
            port = 80
        }

        if opts.Port != 0 {
            port = opts.Port
        }
        if url.Port() != "" {
            port, _ = strconv.Atoi(url.Port())
        }
    }
    
    
    if opts.RequestFile != "" {
        f, err := os.Open(opts.RequestFile)
        if err != nil {
            panic(err)
        }
        defer f.Close()

        var buf bytes.Buffer
        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
            buf.WriteString(scanner.Text())
            buf.WriteString("\n")
        }
        if err := scanner.Err(); err != nil {
            log.Fatal("Failed to read request file: ", err)
        }
        cr = buf.Bytes()
    } else {
        cr = []byte(
            "POST " + opts.URL.Path + " HTTP/1.1\r\n" +
                "Host: " + opts.URL.Host + "\r\n" +
                "Content-Length: 100\r\n" +
                "Connection: keep-alive\r\n\r\n",
        )
    }
            
    return Target{
        URL:    opts.URL,
        TLS:    tls,  
        CurrentReq: cr,
        Port: port,   
    }
}

func showBanner() {
    fmt.Println(`:'#######:::'######::'##::::::::::'########:'##::::'##:'########::'##::::::::'#######::'########::'########:'########::
'##.... ##:'##... ##: ##:::::::::: ##.....::. ##::'##:: ##.... ##: ##:::::::'##.... ##: ##.... ##: ##.....:: ##.... ##:
 ##:::: ##: ##:::..:: ##:::::::::: ##::::::::. ##'##::: ##:::: ##: ##::::::: ##:::: ##: ##:::: ##: ##::::::: ##:::: ##:
 ##:::: ##: ##::::::: ##:::::::::: ######:::::. ###:::: ########:: ##::::::: ##:::: ##: ########:: ######::: ########::
 ##:::: ##: ##::::::: ##:::::::::: ##...:::::: ## ##::: ##.....::: ##::::::: ##:::: ##: ##.. ##::: ##...:::: ##.. ##:::
 ##:::: ##: ##::: ##: ##:::::::::: ##:::::::: ##:. ##:: ##:::::::: ##::::::: ##:::: ##: ##::. ##:: ##::::::: ##::. ##::
. #######::. ######:: ########:::: ########: ##:::. ##: ##:::::::: ########:. #######:: ##:::. ##: ########: ##:::. ##:
:.......::::......:::........:::::........::..:::::..::..:::::::::........:::.......:::..:::::..::........::..:::::..::`)
}


func printUsage() {
    fmt.Fprintf(os.Stderr, "Options:\n")
    fmt.Fprintf(os.Stderr, "  -u        string        Target URL\n")
    fmt.Fprintf(os.Stderr, "  -req        string(file)  A text file of HTTP/1.1 request to start with (optional)\n")
    fmt.Fprintf(os.Stderr, "  -help                   Show this help\n")
}

