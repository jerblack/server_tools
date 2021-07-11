"use strict";
const __ = undefined;
const _ = {
    unchecked: 'crop_square',
    checked: 'checked',
    checks: 'done_all',
    unchecks: 'filter_none',
    refresh: 'refresh',
    link: 'link',
    delete: 'delete',
    save: 'save',
    upload: 'cloud_upload',
    expand: 'keyboard_arrow_down',
    collapse: 'keyboard_arrow_up',
    down: 'keyboard_arrow_down',
    up: 'keyboard_arrow_up',
    left: 'keyboard_arrow_left',
    right: 'keyboard_arrow_right',
    clear: 'clear',
    folder: 'folder',
    file: 'description',
    video: 'ondemand_video',
    subtitles: 'subtitles',
    music: 'music_note',
    close: 'close',
    ok: 'done',
    add: 'add',
    remove: 'remove',
    search: 'search',
    none: 'none',
    unset: 'unset',
    clear_inputs: 'backspace',
    clear_fields: 'clear_all',
    timeout: 10,
    monitored: 'visibility',
    unmonitored: 'visibility_off',
    expando: "unfold_more",
    download: 'get_app',
    scroll_top: 'vertical_align_top',
    select: 'add_circle_outline',
    deselect: 'remove_circle_outline',
    play: 'play_arrow',
    resize: 'vertical_align_center',
    book: 'menu_book',
    clearNotifications: 'clear_all',
    edit: 'edit',
    copy: 'link'
};
Object.freeze(_);
class PartsBin extends HTMLElement {
    get hide() {
        return this.style.display === 'none';
    }
    set hide(tf) {
        this.style.display = tf ? 'none' : '';
    }
    _add_to_me(...elms) {
        for (let e of elms) {
            this.appendChild(e);
        }
    }
    _add_to(ob, ...elms) {
        for (let e of elms) {
            ob.appendChild(e);
        }
    }
    _filler() {
        return document.createElement('filler');
    }
    _stacker(...elms) {
        const s = document.createElement('stacker');
        for (let e of elms) {
            s.appendChild(e);
        }
        return s;
    }
    _scroller() {
        return document.createElement('scroller');
    }
    _row(...elms) {
        let row = document.createElement('row');
        for (let e of elms) {
            row.appendChild(e);
        }
        this.appendChild(row);
        return row;
    }
    _p(text, className) {
        const p = document.createElement('p');
        if (className) {
            p.classList.add(className);
        }
        if (text) {
            p.textContent = text;
        }
        return p;
    }
    _span(text, className) {
        const s = document.createElement('span');
        s.textContent = text;
        if (className) {
            s.classList.add(className);
        }
        return s;
    }
    _img(src, className, alt) {
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
    _div(className, text) {
        const d = document.createElement('div');
        if (className) {
            if (typeof className === 'string') {
                d.classList.add(className);
            }
            else {
                for (let c of className) {
                    d.classList.add(c);
                }
            }
        }
        if (text) {
            d.textContent = text;
        }
        return d;
    }
    _input(placeholder, className) {
        const i = document.createElement("input");
        if (placeholder) {
            i.setAttribute("placeholder", placeholder);
        }
        if (className) {
            i.classList.add(className);
        }
        return i;
    }
}
class NowPlaying extends PartsBin {
    constructor() {
        super(...arguments);
        this.#ui = {};
        this.#state = {};
    }
    #ui;
    #state;
    connectedCallback() {
        this.build();
        NowPlaying._this = this;
    }
    build() {
        const pl = this._div("playlist");
        const r1 = this._row(this._filler(), pl);
        const title = this._div("title");
        const r2 = this._row(title);
        const date = this._div("date");
        const id = this._div("id");
        const r3 = this._row(date, this._filler(), id);
        this._add_to_me(r1, r2, r3);
        this.#ui.title = title;
        title.addEventListener("click", () => {
            core.resizeWindow();
        });
        this.#ui.date = date;
        this.#ui.id = id;
        this.#ui.playlist = pl;
    }
    update(np) {
        if (np.app_loaded == "Backdrop") {
            return;
        }
        this.title = np.title;
        this.date = np.date;
        this.id = np.id;
        this.playlist = np.playlist;
        BtnBox._this.playing = !np.paused;
        TimeMachine._this.percent = np.percent;
        TimeMachine._this.position = np.position;
        TimeMachine._this.duration = np.duration;
    }
    set title(title) {
        if (this.#state.title != title) {
            this.#state.title = title;
            if (title == "") {
                title = "-stopped-";
            }
            this.#ui.title.textContent = title;
            core.resizeWindow();
        }
    }
    set date(date) {
        if (this.#state.date != date) {
            this.#state.date = date;
            this.#ui.date.textContent = date;
        }
    }
    set id(id) {
        if (this.#state.id != id) {
            this.#state.id = id;
            this.#ui.id.innerHTML = id;
        }
    }
    set playlist(playlist) {
        if (this.#state.playlist != playlist) {
            this.#state.playlist = playlist;
            this.#ui.playlist.textContent = playlist;
        }
    }
}
customElements.define('now-playing', NowPlaying);
class TimeMachine extends PartsBin {
    constructor() {
        super(...arguments);
        this.#ui = {};
        this.#state = {};
    }
    #ui;
    #state;
    connectedCallback() {
        this.build();
        TimeMachine._this = this;
    }
    build() {
        const sb = this._div("slider-box");
        const sl = document.createElement("input");
        sl.type = "range";
        sl.min = "0";
        sl.max = "100";
        sl.value = "0";
        this._add_to(sb, sl);
        const pos = this._div("", "00:00:00");
        const dur = this._div("", "00:00:00");
        const row = this._row(pos, this._filler(), dur);
        this._add_to_me(sb, row);
        this.#ui.slider = sl;
        this.#ui.position = pos;
        this.#ui.duration = dur;
        sl.addEventListener("input", () => {
            core.sendCmd("seek", { "type": "percent", "percent": this.percent });
        });
    }
    set percent(p) {
        if (this.#state.pct != p) {
            this.#state.pct = p;
            this.#ui.slider.value = p;
        }
    }
    get percent() {
        return parseInt(this.#ui.slider.value);
    }
    set position(p) {
        if (this.#state.position != p) {
            this.#state.position = p;
            this.#ui.position.textContent = p;
        }
    }
    set duration(p) {
        if (this.#state.duration != p) {
            this.#state.duration = p;
            this.#ui.duration.textContent = p;
        }
    }
}
customElements.define('time-machine', TimeMachine);
class IconButton extends PartsBin {
    static make(icon) {
        const b = document.createElement('icon-btn');
        b.icon = icon;
        return b;
    }
    set icon(icon) {
        this.textContent = icon;
    }
    get icon() {
        return this.textContent;
    }
}
customElements.define('icon-btn', IconButton);
class HeaderMenu extends PartsBin {
    constructor() {
        super(...arguments);
        this.#ui = {};
        this.#state = {};
    }
    #ui;
    #state;
    connectedCallback() {
        this.build();
        HeaderMenu._this = this;
    }
    build() {
        const headerBar = this._div("header-bar");
        const arrow = IconButton.make(_.down);
        const filler = this._filler();
        const title = this._div("header-title", "castctl");
        this._add_to(headerBar, arrow, filler, title);
        this._add_to_me(headerBar);
        this.#ui.headerDD = this._div("header-dd");
        this._add_to_me(this.#ui.headerDD);
    }
    loadMenu(playlists) {
        for (const [k, v] of Object.entries(playlists)) {
            const item = this._div("menu-item", "playlist | " + v["name"]);
            item.addEventListener("click", () => {
                core.sendCmd("start_playlist", { "id": k });
            });
            this._add_to(this.#ui.headerDD, item);
        }
        for (const [k, v] of Object.entries(HeaderMenu.menuItems)) {
            const item = this._div("menu-item", k);
            item.addEventListener("click", () => {
                core.sendCmd(v, {});
            });
            this._add_to(this.#ui.headerDD, item);
        }
    }
}
HeaderMenu.menuItems = {
    "trigger cat videos routine": "cat_videos",
};
customElements.define('header-menu', HeaderMenu);
class RcBtn extends IconButton {
    constructor() {
        super(...arguments);
        this.#state = {};
    }
    #state;
    static make(icon, action) {
        const b = document.createElement('rc-btn');
        b.icon = icon;
        b.action = action;
        return b;
    }
    connectedCallback() {
        this.build();
    }
    build() {
        this.addEventListener("click", () => {
            core.sendCmd(this.#state.action, {});
        });
    }
    set action(cmd) {
        this.#state.action = cmd;
    }
}
customElements.define('rc-btn', RcBtn);
class RcBlank extends PartsBin {
    static make() {
        return document.createElement("rc-blank");
    }
}
customElements.define('rc-blank', RcBlank);
class BtnBox extends PartsBin {
    constructor() {
        super(...arguments);
        this.#ui = {};
        this.#state = {};
    }
    #ui;
    #state;
    connectedCallback() {
        this.build();
        BtnBox._this = this;
    }
    build() {
        const r1 = document.createElement("row");
        const r2 = document.createElement("row");
        const pp = RcBtn.make(_.play, "play_pause");
        this._add_to(r1, RcBtn.make("skip_previous", "prev"), pp, RcBtn.make("skip_next", "next"));
        this._add_to(r2, RcBtn.make("clear", "delete"), RcBtn.make("power_settings_new", "tv_power"), RcBtn.make("stop", "stop"));
        this._add_to_me(r1, r2);
        this.#ui.playBtn = pp;
    }
    set playing(isPlaying) {
        this.#ui.playBtn.textContent = isPlaying ? "pause" : _.play;
    }
}
customElements.define('btn-box', BtnBox);
const core = {
    home: document.getElementById("home"),
    onStartup: () => {
        notify.start();
        core.sendCmd("get_playlists", {}, result => {
            HeaderMenu._this.loadMenu(result);
        });
        core.sendCmd("now_playing", {}, result => {
            NowPlaying._this.update(result);
        });
        setTimeout(core.resizeWindow, 1000);
        navigator.serviceWorker && navigator.serviceWorker.register("worker.js");
    },
    sendCmd: (cmd, args, callback) => {
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
                    output = JSON.parse(output);
                    console.log(output);
                    callback(output);
                }
                else {
                    console.log(output);
                }
            }
        };
        req.send(null);
    },
    resizeWindow: () => {
        if (core.home === null) {
            return;
        }
        const y = core.home.clientHeight + 42;
        const x = core.home.clientWidth + 18;
        window.resizeTo(x, y);
    }
};
window.onload = core.onStartup;
const notify = {
    start: () => {
        let event_source = new EventSource('/events');
        event_source.addEventListener('now_playing', (e) => {
            let msg = JSON.parse(e.data);
            NowPlaying._this.update(msg);
        });
    }
};
//# sourceMappingURL=web.js.map