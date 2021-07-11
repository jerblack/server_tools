declare namespace type {
    interface Notifier {
        start: () => void
        // log: (source: string, text: string) => void
        // error: (source: string, text: string) => void
    }
}

interface Event {
    data:string
}