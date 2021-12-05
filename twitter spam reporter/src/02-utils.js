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

