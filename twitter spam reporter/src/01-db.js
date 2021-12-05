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

