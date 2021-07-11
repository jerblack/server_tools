interface core {
    home:HTMLElement
}
const core = {
    home: document.getElementById("home"),
    onStartup: ():void => {
        notify.start()
        core.sendCmd("get_playlists", {}, result => {
            HeaderMenu._this.loadMenu(result)
        })
        core.sendCmd("now_playing", {}, result => {
            NowPlaying._this.update(result)
        })
        setTimeout(core.resizeWindow, 1000)
        navigator.serviceWorker && navigator.serviceWorker.register("worker.js");

    },
    sendCmd: (cmd: string, args: Object, callback?:(result?:any) => void) => {
        let uri = '/cmd';
        let req = new XMLHttpRequest();
        req.responseType = 'json';
        req.open("post", uri, true);
        req.setRequestHeader('cmd', cmd);
        req.setRequestHeader("args", JSON.stringify(args));
        req.onload = () => {
            let rsp = req.response;
            if (rsp !== null && rsp['greetings human'] === 'hello') {
                let output = rsp['output'];
                if (callback) {
                    output = JSON.parse(output)
                    console.log(output)
                    callback(output);
                } else {
                    console.log(output);
                }
            }
        };
        req.send(null);
    },
    resizeWindow: () => {
        if (core.home === null) {
            return
        }
        const y = core.home.clientHeight + 42
        const x = core.home.clientWidth + 18
        window.resizeTo(x, y)
    }
}
window.onload = core.onStartup

const notify = {
    start: ():void => {
        let event_source = new EventSource('/events');

        event_source.addEventListener('now_playing', (e: Event) => {
            let msg = JSON.parse(e.data);
            NowPlaying._this.update(msg)
        })
    }
}
