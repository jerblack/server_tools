
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


