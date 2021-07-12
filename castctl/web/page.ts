class PartsBin extends HTMLElement {
    get hide(): boolean {
        return this.style.display === 'none';
    }

    set hide(tf: boolean) {
        this.style.display = tf ? 'none' : '';
    }

    _add_to_me(...elms: Array<HTMLElement>) {
        for (let e of elms) {
            this.appendChild(e);
        }
    }

    _add_to(ob: HTMLElement, ...elms: Array<HTMLElement>) {
        for (let e of elms) {
            ob.appendChild(e);
        }
    }

    _filler(): HTMLElement {
        return document.createElement('filler');
    }

    _stacker(...elms: Array<HTMLElement>): HTMLElement {
        const s = document.createElement('stacker');
        for (let e of elms) {
            s.appendChild(e);
        }
        return s;
    }

    _scroller(): HTMLElement {
        return document.createElement('scroller')
    }

    _row(...elms: Array<HTMLElement>) {
        let row = document.createElement('row');
        for (let e of elms) {
            row.appendChild(e);
        }
        this.appendChild(row);
        return row;
    }

    _p(text: string, className?: string): HTMLParagraphElement {
        const p = document.createElement('p');
        if (className) {
            p.classList.add(className);
        }
        if (text) {
            p.textContent = text;
        }
        return p;

    }

    _span(text: string, className?: string): HTMLSpanElement {
        const s = document.createElement('span');
        s.textContent = text;
        if (className) {
            s.classList.add(className);
        }
        return s;
    }

    _img(src: string, className?: string, alt?: string): HTMLImageElement {
        const i = document.createElement('img');
        i.src = src;
        if (className) {
            i.classList.add(className);
        }
        if (alt) {
            i.alt = alt;
        }
        return i;
    }

    _div(className?: string | Array<string> | undefined, text?: string): HTMLDivElement {
        const d = document.createElement('div');
        if (className) {
            if (typeof className === 'string') {
                d.classList.add(className);
            } else {
                for (let c of className) {
                    d.classList.add(c)
                }
            }
        }
        if (text) {
            d.textContent = text;
        }
        return d;
    }

    _input(placeholder?: string, className?: string): HTMLInputElement {
        const i = document.createElement("input")
        if (placeholder) {
            i.setAttribute("placeholder", placeholder)
        }
        if (className) {
            i.classList.add(className)
        }
        return i
    }
}

type NowPlayingUpdate = {
    app_loaded: string
    paused: boolean
    playlist: string
    title: string
    date: string
    id: string
    duration: string
    position: string
    percent: number
    volume: number
    muted: boolean
}

class NowPlaying extends PartsBin {
    static _this:NowPlaying
    #ui: any = {}
    #state: any = {}
    connectedCallback():void {
        this.build()
        NowPlaying._this = this
    }
    build():void {
        const pl = this._div("playlist")
        const r1 = this._row(this._filler(), pl)
        const title = this._div("title")
        const r2 = this._row(title)
        const date = this._div("date")
        const id = this._div("id")
        const r3 = this._row(date, this._filler(), id)
        this._add_to_me(r1, r2, r3)
        this.#ui.title = title
        title.addEventListener("click", () => {
            core.resizeWindow()
        })
        this.#ui.date = date
        this.#ui.id = id
        this.#ui.playlist = pl

    }
    update(np:NowPlayingUpdate):void {
        if (np.app_loaded == "Backdrop") {
            return
        }
        this.title = np.title
        this.date = np.date
        this.id = np.id
        this.playlist = np.playlist
        BtnBox._this.playing = !np.paused
        TimeMachine._this.percent = np.percent
        TimeMachine._this.position = np.position
        TimeMachine._this.duration = np.duration
        core.resizeWindow()

    }
    set title(title:string) {
        if (this.#state.title != title) {
            this.#state.title = title
            if (title == "") {
                title = "-stopped-"
            }
            this.#ui.title.textContent = title
        }
    }
    set date(date:string) {
        if (this.#state.date != date) {
            this.#state.date = date
            this.#ui.date.textContent = date
        }
    }
    set id(id:string) {
        if (this.#state.id != id) {
            this.#state.id = id
            this.#ui.id.innerHTML = id
        }
    }
    set playlist(playlist:string) {
        if (this.#state.playlist != playlist) {
            this.#state.playlist = playlist
            this.#ui.playlist.textContent = playlist
        }
    }
}
customElements.define('now-playing', NowPlaying);

class TimeMachine extends PartsBin {
    static _this:TimeMachine
    #ui: any = {}
    #state: any = {}
    connectedCallback():void {
        this.build()
        TimeMachine._this = this
    }
    build():void {
        const sb = this._div("slider-box")
        const sl = document.createElement("input")
        sl.type = "range"
        sl.min = "0"
        sl.max = "100"
        sl.value = "0"
        this._add_to(sb, sl)
        const pos = this._div("","00:00:00")
        const dur = this._div("","00:00:00")
        const row = this._row(pos, this._filler(), dur)

        this._add_to_me(sb, row)
        this.#ui.slider = sl
        this.#ui.position = pos
        this.#ui.duration = dur

        sl.addEventListener("input", () => {
            core.sendCmd("seek", {"type":"percent", "percent": this.percent})
        })
    }
    set percent(p:number) {
        if (this.#state.pct != p) {
            this.#state.pct = p
            this.#ui.slider.value = p
        }
    }
    get percent():number {
        return parseInt(this.#ui.slider.value)
    }
    set position(p:string) {
        if (this.#state.position != p) {
            this.#state.position = p
            this.#ui.position.textContent = p
        }
    }
    set duration(p:string) {
        if (this.#state.duration != p) {
            this.#state.duration = p
            this.#ui.duration.textContent = p
        }
    }

}
customElements.define('time-machine', TimeMachine);

class IconButton extends PartsBin {
    static make(icon: string):IconButton {
        const b = document.createElement('icon-btn') as IconButton
        b.icon = icon;
        return b
    }
    set icon(icon:string) {
        this.textContent = icon
    }
    get icon():string {
        return <string>this.textContent
    }
}
customElements.define('icon-btn', IconButton);

class HeaderMenu extends PartsBin {
    static _this:HeaderMenu
    #ui: any = {}
    #state: any = {}
    static menuItems = {
        "trigger cat videos routine": "cat_videos",
    }
    connectedCallback():void {
        this.build()
        HeaderMenu._this = this
    }
    build():void {
        const headerBar = this._div("header-bar")
        const arrow = IconButton.make(_.down)
        const filler = this._filler()
        const title = this._div("header-title", "castctl")
        this._add_to(headerBar, arrow, filler, title)
        this._add_to_me(headerBar)

        this.#ui.headerDD = this._div("header-dd")
        // this.#ui.headerDD.style.display = "none"
        this._add_to_me(this.#ui.headerDD)
        // this.addEventListener("click", ()=>{
        //     const current = this.#ui.headerDD.style.display
        //     this.#ui.headerDD.style.display = current == "none" ? "" : "none"
        // })

    }
    loadMenu(playlists: Object):void {
        for (const [k, v] of Object.entries(playlists)) {
            const item = this._div("menu-item", "playlist | " + v["name"])
            item.addEventListener("click", () => {
                core.sendCmd("start_playlist", {"id": k})
            })
            this._add_to(this.#ui.headerDD, item)
        }
        for (const [k, v] of Object.entries(HeaderMenu.menuItems)) {
            const item = this._div("menu-item", k)
            item.addEventListener("click", () => {
                core.sendCmd(v, {})
            })
            this._add_to(this.#ui.headerDD, item)
        }
    }

}
customElements.define('header-menu', HeaderMenu);

// @ts-ignore
class RcBtn extends IconButton {
    #state: any = {}
    static make(icon: string, action: string):RcBtn {
        const b = document.createElement('rc-btn') as RcBtn
        b.icon = icon;
        b.action = action;
        return b
    }
    connectedCallback():void {
        this.build()
    }
    build():void {
        this.addEventListener("click", ()=>{
            core.sendCmd(this.#state.action, {})
        })
    }
    set action(cmd:string){
        this.#state.action = cmd
    }
}
customElements.define('rc-btn', RcBtn);

class RcBlank extends PartsBin {
    static make():RcBlank {
        return document.createElement("rc-blank") as RcBlank
    }
}
customElements.define('rc-blank', RcBlank);

class BtnBox extends PartsBin {
    static _this:BtnBox

    #ui: any = {}
    #state: any = {}
    connectedCallback():void {
        this.build()
        BtnBox._this = this
    }
    build():void {
        const r1 = document.createElement("row")
        const r2 = document.createElement("row")
        const pp = RcBtn.make(_.play, "play_pause")

        this._add_to(r1,
            RcBtn.make("skip_previous", "prev"),
            pp,
            RcBtn.make("skip_next", "next"))
        this._add_to(r2,
            RcBtn.make("clear", "delete"),
            RcBtn.make("power_settings_new", "tv_power"),
            RcBtn.make("stop", "stop"))
        this._add_to_me(r1, r2)
        this.#ui.playBtn = pp
    }
    set playing(isPlaying:boolean) {
        this.#ui.playBtn.textContent = isPlaying ? "pause" : _.play
    }
}
customElements.define('btn-box', BtnBox);
