const baseTermList = []
const baseRegexPatterns = []
const ifOut = out => {if (out) {console.log(out)}}

const db = {
    db: undefined,
    open: async () => {
        db.db = await idb.openDB("twit", 5, {
            upgrade: (db, oldVersion, newVersion, transaction) => {
                if (oldVersion < 1) {
                    db.createObjectStore("monitoredUsers", {keyPath: "user"})
                    db.createObjectStore("keywords", {keyPath: "term"})
                    db.createObjectStore("userGather", {keyPath: "user"})
                    db.createObjectStore("tweetQueue", {keyPath: "tweetId"})
                    db.createObjectStore("reportedTweets", {keyPath: "tweetId"})
                    db.createObjectStore("reportedUsers", {keyPath: "user"})
                }
                if (oldVersion < 2) {
                    db.createObjectStore("regexPatterns", {keyPath: "pattern"})
                }
                if (oldVersion < 4) {
                    db.createObjectStore("unreportedTweets", {keyPath: "tweetId"})
                }
                if (oldVersion < 5) {
                    db.createObjectStore("unreportedTweetIds", {keyPath: "tweetId"})
                }
            }
        })
    },

    addMonitoredUser: async (user, numTweets) => {
        user = user.toLocaleLowerCase()
        return (await db.db).put("monitoredUsers", {user: user, numTweets:numTweets})
    },
    getMonitoredUsers: async () => {
        return (await db.db).getAll("monitoredUsers")
    },
    deleteMonitoredUser: async user => {
        user.user = user.user.toLocaleLowerCase()
        return (await db.db).delete("monitoredUsers", user)
    },
    loadUserGatherQueue: async () => {
        const users = await db.getMonitoredUsers()
        for (let u of users) {
            (await db.db).put("userGather", u)
        }
    },
    getFromGatherQueueByName: async name => {
        name = name.toLocaleLowerCase()
        return (await db.db).get("userGather", name)
    },
    getNextFromUserGatherQueue: async () => {
        const next = await db.db.getAll("userGather", undefined, 1)
        if (next.length > 0) {
            return next[0]
        }
    },
    getUserGatherQueue: async () => {
        return (await db.db).getAll("userGather")
    },
    deleteFromUserGatherQueue: async user => {
        return (await db.db).delete("userGather", user.user)
    },
    clearUserGatherQueue: async () => {
        await db.db.clear("userGather")
    },
    isUserGatherQueueEmpty: async () => {
        const count = await db.db.count("userGather")
        return count === 0
    },
    clearTweetQueue: async () => {
        await db.db.clear("tweetQueue")
    },
    addToTweetQueue: async (tweetId, user) => {
        await db.db.put("tweetQueue", {tweetId: tweetId, user: user})
    },
    isTweetQueueEmpty: async () => {
        const count = await db.db.count("tweetQueue")
        return count === 0
    },
    inTweetQueue: async tweetId => {
        const t = await db.db.get("tweetQueue", tweetId)
        return Boolean(t) && t.tweetId === tweetId
    },
    deleteFromTweetQueue: async tweetId => {
        return (await db.db).delete("tweetQueue", tweetId)
    },
    getNextFromTweetQueue: async () => {
        const next = await db.db.getAll("tweetQueue", undefined, 1)
        return next[0]
    },
    addRegex: async re => {
        if (typeof re === "string") {
            const r = new RegExp(re, "i")
            report.regexPatterns.push(r)
        } else {
            report.regexPatterns.push(re)
            re = re.source
        }
        const exists = await db.db.get("regexPatterns", re)
        if (exists) {
            return
        }
        await db.db.put("regexPatterns", {pattern: re, count: 0})
    },
    incrementRegexCount: async re => {
        const p = await db.db.get("regexPatterns", re)
        p.count += 1
        return (await db.db).put("regexPatterns", p)
    },
    getAllRegex: async () => {
        return (await db.db).getAllKeys("regexPatterns")
    },
    loadAllRegex: async () => {
        const re = await db.getAllRegex()
        let regexPatterns = []
        for (let r of re) {
            regexPatterns.push(new RegExp(r, "i"))
        }
        return regexPatterns
    },
    preloadRegex: async () => {
        for (let re of baseRegexPatterns) {
            await db.addRegex(re)
        }
    },
    addKeyword: async term => {
        term = term.toLocaleLowerCase()
        const kw = await db.db.get("keywords", term)
        if (kw) {
            return
        }

        report.termList.push(term)
        return (await db.db).put("keywords", {term: term, count: 0})
    },
    incrementKeywordCount: async term => {
        term = term.toLocaleLowerCase()
        const kw = await db.db.get("keywords", term)
        kw.count += 1
        return (await db.db).put("keywords", kw)
    },
    getKeyword: async term => {
        term = term.toLocaleLowerCase()
        return (await db.db).get("keywords", term)
    },
    getAllKeywords: async () => {
        return (await db.db).getAllKeys("keywords")
    },
    preloadKeywords: async () => {
        for (let term of baseTermList) {
            await db.addKeyword(term)
        }
    },
    addReportedTweet: async (tweetId, user) => {
        const exists = await db.reportedTweetExists(tweetId)
        if (!exists) {
            await db.db.put("reportedTweets", {tweetId: tweetId, user: user, count: 1})
        } else {
            await db.incrementReportedTweet(tweetId)
        }
    },
    reportedTweetExists: async tweetId => {
        if (!tweetId) {return false}
        const count = await db.db.count("reportedTweets", tweetId)
        return count > 0
    },
    incrementReportedTweet: async tweetId => {
        const t = await db.db.get("reportedTweets", tweetId)
        t.count += 1
        return (await db.db).put("reportedTweets", t)
    },
    addReportedUser: async user => {
        if (user.startsWith("@")) {
            user = user.slice(1)
        }
        const exists = await db.reportedUserExists(user)
        if (!exists) {
            await db.db.put("reportedUsers", { user: user, count: 1})
        } else {
            await db.incrementReportedUser(user)
        }
    },
    reportedUserExists: async user => {
        if (!user) {return false}
        const count = await db.db.count("reportedUsers", user)
        return count > 0
    },
    incrementReportedUser: async user => {
        const u = await db.db.get("reportedUsers", user)
        u.count += 1
        return (await db.db).put("reportedUsers", u)
    },
    fixUnreported: async () => {
    /*  get all ids from unreportedTweetIds
    *   get all tweet_ids from unreportedTweets
    *   for each id in ids
    *      if exists in tweet_ids then delete tweet from unreportedTweets
    *   get all tweet_ids from unreportedTweets
    *   for each tweet_id in tweet_ids
    *      if not exists in ids then add id to unreportedTweetIds
    * */
        console.log("starting fixUnreported")
        console.log("getting ids")
        const ids = await db.db.getAllKeys("unreportedTweetIds")
        console.log("getting tweetIds")
        let tweetIds = await db.db.getAllKeys("unreportedTweets")
        console.log("checking for seen ids in current batch of tweetIds")
        for (let id of ids) {
            if (tweetIds.includes(id)) {
                console.log(`tweetIds contains seen id ${id}. removing from batch`)
                await db.db.delete("unreportedTweets", id)
            }
        }
        console.log("done checking for seen ids in current batch of tweetIds")
        console.log("refreshing tweetIds")
        tweetIds = await db.db.getAllKeys("unreportedTweets")
        console.log("marking tweets in batch in id list")
        for (let tweetId of tweetIds) {
            if (!ids.includes(tweetId)) {
                console.log(`batch tweet with id ${tweetId} not found in ids list. adding now`)
                await db.addUnreportedTweetId(tweetId)
            }
        }
        console.log("fixUnreported is finished")
    },

    addUnreportedTweet: async (tweetId, user, text) => {
        // unreportedTweets
        let exists = await db.unreportedTweetIdExists(tweetId)
        if (!exists) {
            await Promise.allSettled([
                db.db.put("unreportedTweets", {tweetId: tweetId, user: user, text: text}),
                db.addUnreportedTweetId(tweetId)
            ])
        }
    },
    printUnreportedTweets : async n => {
        if (typeof n !== 'number' || n < 1 ) {
            return
        }
        let tweets = await db.db.getAll("unreportedTweets", undefined, n)
        let proms = []
        for (let i = 0; i < n ; i++) {
            let t = tweets[i]
            if (t) {
                console.log(t.text)
                proms.push(db.db.delete("unreportedTweets", t.tweetId))
            }
        }
        let promSettled = await Promise.allSettled(proms)
        for (let p of promSettled) {
            if (p.status !== 'fulfilled') {console.log(p)}
        }
        return "-".repeat(60)
    },
    addUnreportedTweetId: async tweetId => {
        return (await db.db).put("unreportedTweetIds", {tweetId: tweetId})
    },
    unreportedTweetIdExists: async tweetId => {
        let count = await db.db.count("unreportedTweetIds", tweetId)
        return count > 0
    },
    preloadUnreportedTweetIds: async () => {
        let ids = await db.db.getAllKeys("unreportedTweets")
        for (let id of ids) {
            await db.addUnreportedTweetId(id)
        }
    }
}


const view = {
    keywords: async () => {
        const div = document.createElement("div")
        div.classList.add("twit-spam-overlay")
        let text = await db.db.getAllKeys("keywords")
        div.textContent = JSON.stringify(text)
        document.body.appendChild(div)
    }
}

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}
const abort = () => {
    return window.location.pathname.includes("jerblack")
}

const reloadPaqe = () => {
    window.location.href = window.location.href
}
const reset = async () => {
    /*
    *   clear all tweets from tweetQueue
    *   clear all users from userGather
    * */
    await db.clearTweetQueue()
    await db.clearUserGatherQueue()
    reloadPaqe()

}


const elmHasText = (elm, text) => {
    if (!text) {
        text = [...termList]
    }
    if (typeof text === "string") {
        text = [text]
    }
    for (let t of text) {
        if (elm.textContent.toLowerCase().includes(t.toLowerCase())) {
            console.log(`found search term -|${t}|- in tweet`)
            return true
        }
    }
    return false
}

const getUserFromTweet = (elm) => {
    let a = elm.querySelector("div[class='css-1dbjc4n r-1wbh5a2 r-dnmrzs'] a")
    if (a) {
        return a.pathname.slice(1).toLocaleLowerCase()
    }
}

const getIdFromTweet = (elm) => {
    let a = elm.querySelector("a.css-4rbku5.css-18t94o4.css-901oao.r-9ilb82.r-1loqt21.r-1q142lx.r-1qd0xha.r-a023e6.r-16dba41.r-rjixqe.r-bcqeeo.r-3s2u2q.r-qvutc0")
    if (!a) {
        a = elm.querySelector("a[class='css-4rbku5 css-18t94o4 css-901oao css-16my406 r-9ilb82 r-1loqt21 r-poiln3 r-bcqeeo r-qvutc0']")
    }
    if (a) {
        return a.href.split("/").pop()
    }

}
const getIdFromIframe = () => {
    let iframes = document.getElementsByTagName("iframe")
    if (iframes) {
        let iframe = iframes[0]
        let queries = iframe.src.split("&")
        for (let q of queries) {
            if (q.includes("=")) {
                let [k,v] = q.split("=")
                if (k==="reported_tweet_id") {
                    return v
                }
            }
        }
    }
}
const getIdFromUri = () => {
    const id = window.location.href.split("/").pop()
    if (/^\d+$/i.test(id)) {
        return id
    }
}
const getUserFromUserPageURI = () => {
    const parts = window.location.pathname.split("/")
    if (parts[0] === "" && parts.length === 3 && parts[2] === "with_replies") {
        return parts[1].toLocaleLowerCase()
    }
}
const getUserFromTweetPageURI = () => {
    const parts = window.location.pathname.split("/")
    if (parts[0] === "" && parts.length === 4 && parts[2] === "status") {
        return parts[1].toLocaleLowerCase()
    }
}


const reportAll = async () => {
    if (await db.isUserGatherQueueEmpty()) {
        await db.loadUserGatherQueue()
    }
    await pager.isQueueActive()
}

const pager = {
    numTweets: 30,
    currentUser: undefined,
    isQueueActive: async () => {
        if (abort()) {return}
        const userGatherEmpty = await db.isUserGatherQueueEmpty()
        if (!userGatherEmpty) {
            console.log("user gather not empty")
            const user = await pager.loadNextUserPage()
            pager.currentUser = user
            console.log("current page is user page for " + user.user)
            await sleep(4000)
            await pager.hideHeader()
            await pager.tweetGather(user)
            await db.deleteFromUserGatherQueue(user)
            await pager.loadNextUserPage()
        }
        const tweetQueueEmpty = await db.isTweetQueueEmpty()
        if (!tweetQueueEmpty) {
            const id = getIdFromUri()
            if (id && await db.inTweetQueue(id)) {
                let ok = await report.start()
                if (!ok) {return}
                await db.deleteFromTweetQueue(id)
            }
            const tw = await db.getNextFromTweetQueue()
            if (tw) {
                window.location.href = `https://twitter.com/${tw.user}/status/${tw.tweetId}`
            }
        }
        console.log("tweet queue empty")
    },
    loadNextUserPage: async () => {
        const currentUser = getUserFromUserPageURI()
        if (currentUser) {
            const user = await db.getFromGatherQueueByName(currentUser)
            console.log("lnup user: "+ user.user)
            console.log("lnup currentUser: " + currentUser)
            if (user) {
                console.log("lnup match return")
                return user
            }
        }

        const nextUser = await  db.getNextFromUserGatherQueue()
        if (nextUser) {
            console.log("lnup users length > 0, loading to next page")
            window.location.href = `https://twitter.com/${nextUser.user}/with_replies`
            return nextUser
        }
    },
    hideHeader: () => {
        const hdr = document.querySelector("div.css-1dbjc4n.r-1jgb5lz.r-1ye8kvj.r-13qz1uu div.css-1dbjc4n.r-1jgb5lz.r-1ye8kvj.r-13qz1uu > div.css-1dbjc4n")
        hdr.style.display = "none"
    },
    tweetGather: async user => {
        let totalCount = 0
        let count = -1
        const doList = async () => {
            const list = document.querySelector("section[aria-labelledby^='accessible-list-'] > div > div")
            count = 0
            for (const elm of list.childNodes) {
                if (elm.style.display === "none") {
                    continue
                }

                if (elm.textContent === "") {
                    await sleep(400)
                    console.log("collapsing unrelated ui")
                    elm.style.display = "none"
                    continue
                }

                const twUser = getUserFromTweet(elm)
                if (twUser === user.user) {
                    const twId = getIdFromTweet(elm)
                    await db.addToTweetQueue(twId, twUser)
                    elm.scrollIntoView()
                    elm.style.display = "none"
                    totalCount++
                    count++
                    console.log(`logging tweet ${totalCount} with id ${twId} | count now ${count}`)
                    if (totalCount >= user.numTweets) {
                        return
                    }
                    await sleep(400)
                    continue
                }
                await sleep(400)
                console.log("collapsing unrelated tweet")
                elm.style.display = "none"
            }
        }
        while (count !== 0 && totalCount <= user.numTweets) {
            await sleep(2000)

            await doList()
        }

    }
}


const printAllTweets = async () => {
    const list = document.querySelector("section[aria-labelledby^='accessible-list-'] > div > div")
    const tweetUser = getUserFromTweetPageURI()
    for (const elm of list.childNodes) {
        if (elm.style.display === "none") {
            continue
        }
        await sleep(200)
        elm.scrollIntoView()
        if (elm.textContent.startsWith("Including results for") || elm.textContent === "") {
            elm.style.display = "none"
            continue
        }
        if (elm.textContent.startsWith("You reported this Tweet")) {
            elm.style.display = "none";
            continue
        }
        if (elm.textContent === 'Show more replies') {
            const btn = elm.querySelector("div[role='button']")
            if (btn) {
                btn.click()
                await sleep(5000)
            } else {
                elm.style.display = "none";
            }
            continue
        }
        if (elm.textContent.startsWith("Show additional replies, including")) {
            const btn = elm.querySelector("div[role='button']")
            if (btn) {
                console.log("clicking Show additional replies, including... button")
                btn.click()
                await sleep(5000)
            } else {
                elm.style.display = "none";
            }
            continue
        }
        const user = getUserFromTweet(elm)
        if (user === tweetUser) {
            elm.style.display = "none"
            continue
        }
        const text = elm.querySelector("div[class='css-901oao r-1fmj7o5 r-1qd0xha r-a023e6 r-16dba41 r-rjixqe r-bcqeeo r-bnwqim r-qvutc0'] span").textContent
        console.log(text)
        elm.style.display = "none"
    }
}


const report = {
    termList: [],
    regexPatterns: [],
    list:[],
    count:-1,
    stop: false,
    pause: false,
    start: async () => {
        if (abort()) {return}

        report.termList = await db.getAllKeywords()
        report.regexPatterns = await db.loadAllRegex()
        await sleep(2000)
        report.list = document.querySelector("section[aria-labelledby^='accessible-list-'] > div > div")
        while (report.count !== 0) {
            let ok = await report.processList()
            if (!ok) {
                console.log("problem reported. waiting 10 seconds and retrying")
                await sleep(10000)
                report.count = -1
            } else {
                window.scrollTo(0,document.body.scrollHeight);
                await sleep(1000)
            }
        }
        return true
    },
    elmHasText : async elm => {
        const text = elm.textContent.toLowerCase()
        for (let t of report.termList) {
            if (text.includes(t.toLowerCase())) {
                await db.incrementKeywordCount(t)
                console.log(`found search term -|${t}|- in tweet`)
                return true
            }
        }
        return false
    },
    elmHasRegex : async elm => {
        const text = report.getTextFromElm(elm)
        if (text) {
            for (let re of report.regexPatterns) {
                if (re.test(text)) {
                    await db.incrementRegexCount(re.source)
                    return true
                }
            }
        }
        return false
    },
    getTextFromElm : elm => {
        const textBox = elm.querySelector("div[class='css-901oao r-1fmj7o5 r-1qd0xha r-a023e6 r-16dba41 r-rjixqe r-bcqeeo r-bnwqim r-qvutc0'] span")
        if (textBox) {
            return textBox.textContent
        }
    },
    processList: async () => {
        report.count = 0
        for (const elm of report.list.childNodes) {
            if (report.stop) {
                console.log("manual stop detected")
                report.count = 0
                return false
            }
            if (report.pause) {
                console.log("manual pause detected")
                await sleep(30000)
                console.log("resuming from pause")
                report.pause=false
            }
            if (elm.style.display === "none") {
                continue
            }
            await sleep(500)
            elm.scrollIntoView()
            report.count++
            if (elm.textContent.startsWith("Including results for") || elm.textContent === "") {
                console.log("collapsing unrelated ui")
                elm.style.display = "none"
                continue
            }
            if (elm.textContent.startsWith("You reported this Tweet")) {
                elm.style.display = "none";
                console.log("collapsing reported tweet")
                continue
            }
            if (elm.textContent === 'Show more replies' || elm.textContent === 'Show replies') {
                const btn = elm.querySelector("div[role='button']")
                if (btn) {
                    console.log("clicking Show more replies button")
                    btn.click()
                    await sleep(5000)
                } else {
                    elm.style.display = "none";
                }
                continue
            }
            if (elm.textContent.startsWith("Show additional replies, including")) {
                const btn = elm.querySelector("div[role='button']")
                if (btn) {
                    console.log("clicking Show additional replies, including... button")
                    btn.click()
                    await sleep(5000)
                } else {
                    elm.style.display = "none";
                }
                continue
            }
            const id = getIdFromTweet(elm)
            if (await db.reportedTweetExists(id)) {
                console.log(`tweet with id ${id} already reported`)
                elm.style.display = "none"
                continue
            }
            const user = getUserFromTweet(elm)
            if (await report.elmHasText(elm)) {
                console.log("reporting tweet with term matching text : " + elm.textContent)
                let problem = await report.report(elm, user, id)
                if (problem) {
                    console.log("problem during report")
                    return false
                }
                elm.style.display = "none"
                continue
            }

            if (await report.elmHasRegex(elm)) {
                console.log("reporting tweet with regex matching text : " + elm.textContent)
                let problem = await report.report(elm, user, id)
                if (problem) {
                    console.log("problem during report")
                    return false
                }
                elm.style.display = "none"
                continue
            }

            if (await db.reportedUserExists(user)) {
                console.log(`reporting tweet from known spammer ${user}`)
                let problem = await report.report(elm, user, id)
                if (problem) {
                    console.log("problem during report")
                    return false
                }
                elm.style.display = "none"
                continue
            }
            db.addUnreportedTweet(id, user, elm.textContent).then(out => {
                if (out) {console.log(out)}
            })
            console.log("collapsing unrelated tweet with text : " + elm.textContent )
            elm.style.display = "none"
        }
        return true
    },
    report: async (elm, user, id) => {
        console.log(`reporting tweet from ${user} with id ${id}`)
        const caret = elm.querySelector("div[data-testid='caret']")
        console.log("clicking menu btn")
        caret.click()

        let problem = await report.clickReportTweet()
        if (!problem) {
            problem = await report.clickItsSuspiciousOrSpam()
        }
        if (!problem) {
            problem = await report.clickUsesTheReplyFunctionToSpam()
        }

        if (!problem) {
            await Promise.allSettled([
                db.addReportedUser(user),
                db.addReportedTweet(id, user),
            ])
            problem = await report.clickDoneButton()
        }
        return problem

    },
    clickReportTweet: async () => {
        console.log("click report tweet in tweet context menu")
        const selector = "div[data-testid='report']"
        await sleep(1000)
        let reportBtn = document.querySelector(selector)
        if (reportBtn) {
            console.log("clicking report btn")
            reportBtn.click()
            return false
        } else {
            console.log("waiting for report button")
            await sleep(3000)
            reportBtn = document.querySelector(selector)
            console.log("clicking report btn #2")
            if (reportBtn) {
                reportBtn.click()
                return false
            } else {
                console.log("no report btn to click")
                await report.clickLayer()
                return true
            }
        }
    },
    clickItsSuspiciousOrSpam: async () => {
        console.log("click its suspicious or spam button")
        let problem = await report.handleErrors()
        if (problem) {
            await report.clickLayer()
            return true
        }
        await sleep(1000)
        let spam = await report.getFromIframe("button#spam-btn")
        console.log("clicking spam btn")
        if (spam) {
            spam.click()
            return false
        } else {
            console.log("waiting for spam button")
            await sleep(5000)
            spam = await report.getFromIframe("button#spam-btn")
            if (spam) {
                console.log("clicking spam btn #2")
                spam.click()
                return false
            } else {
                console.log("no spam btn to click")
                await report.clickLayer()
                return true
            }
        }
    },
    clickUsesTheReplyFunctionToSpam: async () => {
        console.log("click uses the reply function to spam button")
        let problem = await report.handleErrors()
        if (problem) {
            await report.clickLayer()
            return true
        }
        await sleep(1000)
        let replyBtn = await report.getFromIframe("button#engagement-btn")
        console.log("clicking reply btn")
        if (replyBtn) {
            replyBtn.click()
            return false
        } else {
            console.log("waiting for reply button")
            await sleep(5000)
            replyBtn = await report.getFromIframe("button#engagement-btn")
            if (replyBtn) {
                console.log("clicking reply btn #2")
                replyBtn.click()
                return false
            } else {
                console.log("no reply btn to click")
                await report.clickLayer()
                return true
            }
        }
    },
    clickDoneButton: async () => {
        console.log("click done button in upper right corner")
        let problem = await report.handleErrors()
        if (problem) {
            await report.clickLayer()
            return true
        }
        await sleep(1000)
        let done = document.getElementsByClassName("css-18t94o4 css-1dbjc4n r-42olwf r-sdzlij r-1phboty r-rs99b7 r-1ceczpf r-lp5zef r-1ny4l3l r-1e081e0 r-o7ynqc r-6416eg r-lrvibr")[0]
        if (done) {
            console.log("clicking done btn")
            done.click()
            return false
        } else {
            await report.handleErrors()
            await sleep(2000)
            done = document.getElementsByClassName("css-18t94o4 css-1dbjc4n r-42olwf r-sdzlij r-1phboty r-rs99b7 r-1ceczpf r-lp5zef r-1ny4l3l r-1e081e0 r-o7ynqc r-6416eg r-lrvibr")[0]
            if (done) {
                console.log("clicking done btn #2")
                done.click()
                return false
            } else {
                console.log("no done btn to click")
                await report.clickLayer()
                return true
            }
        }
    },
    getFromIframe: async selector => {
        try {
            const iframeTag = document.querySelector("iframe.r-1yadl64.r-16y2uox")
            if (iframeTag) {
                const iframe = iframeTag.contentDocument || iframeTag.contentWindow.document;
                return iframe.querySelector(selector)
            }
        } catch (e) {
            console.error(e.message)
            console.log("error occurred. reloading page in 10 seconds")
            await sleep(10000)
            reloadPaqe()
        }

    },
    errCount: 0,
    handleErrors: async () => {
        console.log("checking on error")
        await sleep(1000)
        const error = await report.getFromIframe("div.error-page-title")
        if (error) {
            console.log("error is ", error)
            report.errCount ++
            if (report.errCount >= 5) {
                reloadPaqe()
            }
            console.log("detected error. waiting 10 seconds before continuing")
            await sleep(10000)
            return false
        }
        // https://twitter.com/search?src=recent_search_click&f=live&q=BITPRIZE%20dot%20NET
        const notFound = await report.getFromIframe("h1#header")
        if (notFound && notFound.textContent === 'Nothing to see here') {
            // console.log(('detected 404 on report iframe. reloading page'))
            // await sleep(2000)
            // window.location.href = "https://twitter.com/search?src=recent_search_click&f=live&q=" + localStorage.report
            let id = getIdFromIframe()
            if (id) {
                console.log("detected 404 on report iframe. marking tweet as skipped")
                return true
            }
        }
        report.errCount = 0
    },
    clickLayer: async () => {
        await report.handleErrors()
        for (let i=1;i<11;i++) {
            const layer = document.querySelector("#layers > div:nth-child(2) > div > div > div > div > div > div.css-1dbjc4n.r-1awozwy.r-18u37iz.r-1pi2tsx.r-1777fci.r-1xcajam.r-ipm5af.r-g6jmlv > div.css-1dbjc4n.r-11z020y.r-1p0dtai.r-1d2f490.r-1xcajam.r-zchlnj.r-ipm5af")
            if (layer) {
                console.log("clicking back layer #"+i)
                layer.click()
            } else {
                console.log("no layer found")
                return
            }
            await sleep(1000)
        }
    },
    clickBlock: async () => {
        const block = await report.getFromIframe("button[id='block-btn']")
        if (block) {
            console.log("clicking block btn")
            block.click()
        } else {
            console.log("no block btn found")
        }
    }

}


window.addEventListener("load", async () => {
    await db.open()
    await pager.isQueueActive()
})

const setup = async () => {
    await Promise.all([
        db.preloadKeywords(),
        db.addMonitoredUser("elonmusk"),
        db.addMonitoredUser("spacex"),
        db.addMonitoredUser("tesla")
    ])
}

const addKeywords = (...terms) => {
    for (let term of terms) {
        db.addKeyword(term).then(ifOut)
    }
}
const addKeyword = addKeywords

const addRegex = re => {
    db.addRegex(re).then(ifOut)
}
const addPattern = addRegex
const stop = () => {
    report.stop = true
}
const addSpammer = user => {
    db.addReportedUser(user).then(ifOut)
}
const clear = () => {
    db.clearTweetQueue().then(ifOut)
}
const pause = () => {
    report.pause = true
}
const print = n => {
    db.printUnreportedTweets(n).then(ifOut)
}

const x = {
    print: print,
    stop: stop,
    pause: pause,
    addRegex: addRegex,
    addKeyword: addKeyword,
    addKeywords: addKeywords,
    clear: clear,
    addSpammer: addSpammer
}
