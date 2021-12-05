function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

let termList = [
    "bit dot ly/3bmzctt", "floki_real", "shiba_cash","fastswapdex","$gshib","$boss","catgirlcoin", "brbcrypto", "$saninu",
    "crpyear dot net","vancat", "$dinger","dogearmy","skbala07068619", "$starl", "$trees", "$shak","$sato","londexarmy",
    "marstogo", "cryptooxide", "stockmarketpump", "kishuarmy","$ghc", "spacex & tesla bounty","$crb","arivacoin",
    "opensea.io","moonrabbitaz","$aaa","shibafloki", "big_eth", "avastarter","xenonplay","xenonkart","cryptoartplan",
    "gemx2airdrop.blogspot","$jasmy","space x bounty","sparklab","crypfans dot net","#shib","metabunnyrocket",
    "bitprize dot net","a fr3sh [airdro0p]", "fresh airdrop","rewardhunters.finance","$raca","dogenode","$mmat",
    "fastswapdex", "santa_coins", "#grt","tokenryoshi","trustwallet","poocoin","x_aea12","galaxy heroes",
    "bcryp dot net","polyx","crypt00","$ltmcf","buffedshiba","$dyna","cryptodingo","flokimoon",
    "kishimoto","shubarmy","hey_wallet","solscan.io","$squid","shibarocketdog",
    "enaira","$ghc","shibtoken","roxtoken","$feg","metascorpio", "nftbooks","galaxyheroeghc",
    "elemongame","bprize dot site", "flokiloki","coingecko","bloktopia","x2gift","crypgifts",
    "elemon", "stakecube.net","$shib", "minidoge","mommydoge", "boss__token","dogecoin moon",
    "babydoge","dogezero", "karencoin","dogeyinutweets", "fuddexx","fuddox","zoo zoo","luffy coin",
    "buffdogecoin","cryptoisland","cisla","shibacoin", "hamstercoin","kabosutoken.io",
    "cryptohippies","randomearth.io","solubagchile", "bondlyfinance","jts_global","tesla & spacex bounty",
    "upfi_network","galaxyheroescoin","nanodogecoin","dogira","rocketridersnft","saitama coin",
    "flokinomix","kabosu","afrostar","hubitothemoon","$ssb","buffedshiba","babydogearmy",
    "$floki","moontimergame","starlinketh", "galaxyheroescoin", "galaxyherocoin","babyethproject",
    "freecoin","herofloki","xxxnifty","buffdogecoin","calvin_crypto1","giveaway 50$","ghc",
    "dogezilla","bloktopia","chia_project","flokiinu","realhinainu","shibosu","x100gem",
    "yup_io","#shiba","$cns","tripcandyio","$candy","xrdoge","lildogefloki","shiba event",
    "binance","linksync","dogememegirl","shiblite","#richquack","$root","dogegf","$kuma",
    "safemoonzilla","shibaexchange","dextools.io","albonft","shiborg","dogezzilla",
    "moomooapp","trollercoin","$saita","obribt_chain","fegtoken","spikeinu","marsshiba",
    "sendprize dot net", "shiba it’s big scam","shibakitainu", "shitzuinu.io", "really airdrop",
    "buffdogecoin","palette token","shibgame","yooshi̇token","$dogo","babymoonfloki",
    "position exchange",'uptober',"position.exchange","www.saylorevent.org","0x66313c839749bc26adb2fff98298479950303a07",
    "#babyethereum", "$xyo", "block_monsters", "ainutoken", "crypgive", "alpha doge", "shib to the moon"
]

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


let lastElm = undefined
let skippedTweets = []
let spamUsers = []
let hideCount = 0
async function getTweet(text, count) {
    const list = document.querySelector("section[aria-labelledby^='accessible-list-'] > div > div")
    let returnElm = undefined
    let breakException = {}
    if (text) {
        text = text.toLowerCase()
    }
    try {
        for (const elm of list.childNodes) {
            lastElm = elm
            if (elm.style.display === "none") {
                continue
            }
            let id = getIdFromTweet(elm)
            if (skippedTweets.includes(id)) {
                elm.style.display = "none"
                continue
            }

            if (elm.textContent.startsWith("Including results for") || elm.textContent === "") {
                await sleep(200)
                console.log("collapsing unrelated ui")
                elm.style.display = "none"
            } else if (elm.textContent.startsWith("You reported this Tweet")) {
                await sleep(200)
                elm.style.display = "none";
                hideCount++
                console.log("collapsing reported tweet " + hideCount)
            } else if (elmHasText(elm, text)) {
                returnElm = elm
                elm.scrollIntoView()
                let user = getUserFromTweet(elm)
                if (user) {
                    spamUsers.push(user)
                }
                // throw exception to stop looking
                throw breakException;
            } else if (hasSpammer(elm)) {
                console.log("found tweet from known spammer")
                returnElm = elm
                elm.scrollIntoView()
                // throw exception to stop looking
                throw breakException;

            } else if (elm.textContent === 'Show more replies') {
                const btn = elm.querySelector("div[role='button']")
                if (btn) {
                    console.log("clicking Show more replies button")
                    btn.click()
                    await sleep(5000)
                }
            } else if (elm.textContent.startsWith("Show additional replies, including")) {
                const btn = elm.querySelector("div[role='button']")
                if (btn) {
                    console.log("clicking Show additional replies, including... button")
                    btn.click()
                    await sleep(5000)
                }
            }  else {
                await sleep(200)
                console.log("collapsing unrelated tweet")
                elm.style.display = "none"
            }
        }
    } catch(e) {
        if (e!==breakException) throw e;
    }
    if (count === undefined) {
        count = 0
    }
    if (!returnElm && list.childNodes.length > 0 && count < 10) {
        count++
        console.log("all visible tweets reported, retrying " + count)
        await sleep(1000)
        return await getTweet(text, count)
    }
    return returnElm
}

const hasSpammer = (elm) => {
    let user = getUserFromTweet(elm)
    return spamUsers.includes(user)
}
const getUserFromTweet = (elm) => {
    let a = elm.querySelector('a[class="css-4rbku5 css-18t94o4 css-1dbjc4n r-kemksi r-sdzlij r-1loqt21 r-1adg3ll r-h3s6tt r-1ny4l3l r-1udh08x r-o7ynqc r-6416eg"]')
    if (a) {
        return a.pathname.slice(1)
    }
}

const getIdFromTweet = (elm) => {
    let a = elm.querySelector("a.css-4rbku5.css-18t94o4.css-901oao.r-9ilb82.r-1loqt21.r-1q142lx.r-1qd0xha.r-a023e6.r-16dba41.r-rjixqe.r-bcqeeo.r-3s2u2q.r-qvutc0")
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

// window.addEventListener("load", async () => {
//     if (localStorage.report) {
//         await sleep(5000)
//         console.log("restarting tweet reporter with term: "+localStorage.report)
//         await report(localStorage.report)
//     }
// })

let STOP = false
async function report(text) {
    localStorage.setItem("report", text)
    let t = await getTweet(text)
    while (t) {
        if (STOP === true) {
            return
        }
        let problem = false
        const caret = t.querySelector("div[data-testid='caret']")
        console.log("clicking menu btn")
        caret.click()

        problem = await clickReportTweet()
        if (!problem) {
            problem = await clickItsSuspiciousOrSpam()
        }
        if (!problem) {
            problem = await clickUsesTheReplyFunctionToSpam()
        }
        if (!problem) {
            problem = await clickDoneButton()
        }

        await sleep(1000)
        t = await getTweet(text)
        if (!t) {
            console.log("no tweets in view. trying to scroll down for more")
            window.scrollTo(0,document.body.scrollHeight);
            await sleep(1000)
            t = await getTweet(text)
        }
    }
    console.log("no more tweets")
    localStorage.removeItem("report")
}

const clickReportTweet = async () => {
    console.log("click report tweet in tweet context menu")
    const selector = "div[data-testid='report']"
    await sleep(1000)
    let report = document.querySelector(selector)
    if (report) {
        console.log("clicking report btn")
        report.click()
        return false
    } else {
        console.log("waiting for report button")
        await sleep(3000)
        report = document.querySelector(selector)
        console.log("clicking report btn #2")
        if (report) {
            report.click()
            return false
        } else {
            console.log("no report btn to click")
            await clickLayer()
            return true
        }
    }
}
const clickItsSuspiciousOrSpam = async () => {
    console.log("click its suspicious or spam button")
    let problem = await handleErrors()
    if (problem) {
        await clickLayer()
        return true
    }
    await sleep(1000)
    let spam = getFromIframe("button#spam-btn")
    console.log("clicking spam btn")
    if (spam) {
        spam.click()
        return false
    } else {
        console.log("waiting for spam button")
        await sleep(5000)
        spam = getFromIframe("button#spam-btn")
        if (spam) {
            console.log("clicking spam btn #2")
            spam.click()
            return false
        } else {
            console.log("no spam btn to click")
            await clickLayer()
            return true
        }
    }
}
const clickUsesTheReplyFunctionToSpam = async () => {
    console.log("click uses the reply function to spam button")
    let problem = await handleErrors()
    if (problem) {
        await clickLayer()
        return true
    }
    await sleep(1000)
    let replyBtn = getFromIframe("button#engagement-btn")
    console.log("clicking reply btn")
    if (replyBtn) {
        replyBtn.click()
        return false
    } else {
        console.log("waiting for reply button")
        await sleep(5000)
        replyBtn = getFromIframe("button#engagement-btn")
        if (replyBtn) {
            console.log("clicking reply btn #2")
            replyBtn.click()
            return false
        } else {
            console.log("no reply btn to click")
            await clickLayer()
            return true
        }
    }
}

const clickDoneButton = async () => {
    console.log("click done button in upper right corner")
    let problem = await handleErrors()
    if (problem) {
        await clickLayer()
        return true
    }
    await sleep(1000)
    let done = document.getElementsByClassName("css-18t94o4 css-1dbjc4n r-42olwf r-sdzlij r-1phboty r-rs99b7 r-1ceczpf r-lp5zef r-1ny4l3l r-1e081e0 r-o7ynqc r-6416eg r-lrvibr")[0]
    if (done) {
        console.log("clicking done btn")
        done.click()
        return false
    } else {
        await handleErrors()
        await sleep(2000)
        done = document.getElementsByClassName("css-18t94o4 css-1dbjc4n r-42olwf r-sdzlij r-1phboty r-rs99b7 r-1ceczpf r-lp5zef r-1ny4l3l r-1e081e0 r-o7ynqc r-6416eg r-lrvibr")[0]
        if (done) {
            console.log("clicking done btn #2")
            done.click()
            return false
        } else {
            console.log("no done btn to click")
            await clickLayer()
            return true
        }
    }
}



const getFromIframe = selector => {
    const iframeTag = document.querySelector("iframe.r-1yadl64.r-16y2uox")
    const iframe = iframeTag.contentDocument || iframeTag.contentWindow.document;
    return iframe.querySelector(selector)
}

const handleErrors = async () => {
    console.log("checking on error")
    await sleep(1000)
    const error = getFromIframe("div.error-page-title")
    console.log("error is ", error)
    if (error) {
        console.log("detected error. waiting 10 seconds before continuing")
        await sleep(10000)
        return false
    }
    // https://twitter.com/search?src=recent_search_click&f=live&q=BITPRIZE%20dot%20NET
    const notFound = getFromIframe("h1#header")
    if (notFound && notFound.textContent === 'Nothing to see here') {
        // console.log(('detected 404 on report iframe. reloading page'))
        // await sleep(2000)
        // window.location.href = "https://twitter.com/search?src=recent_search_click&f=live&q=" + localStorage.report
        let id = getIdFromIframe()
        if (id) {
            console.log("detected 404 on report iframe. marking tweet as skipped")
            skippedTweets.push(id)
            return true
        }
    }
}


const clickLayer = async () => {
    await handleErrors()
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
}
const clickBlock = () => {
    const block = getFromIframe("button[id='block-btn']")
    if (block) {
        console.log("clicking block btn")
        block.click()
    } else {
        console.log("no block btn found")
    }
}

const core = {
    sendCmd: (cmd, args, cb) => {
        if (!args) {
            args = {};
        }
        let uri = 'http://luna:8080/cmd';
        let req = new XMLHttpRequest();
        let json = JSON.stringify(args);
        json = json.replace(/[\u007F-\uFFFF]/g, function (chr) {
            return "\\u" + ("0000" + chr.charCodeAt(0).toString(16)).substr(-4);
        });
        req.responseType = 'json';
        req.open("post", uri, true);
        req.setRequestHeader('cmd', cmd);
        req.setRequestHeader("args", json);
        req.onload = () => {
            let rsp = req.response;
            if (rsp !== null && rsp['greetings human'] === 'hello') {
                let output = rsp['output'];
                if (cb) {
                    console.log(output);
                    cb(output);
                }
                else {
                    console.log(output);
                }
            }
        };
        req.send(null);
    }
}