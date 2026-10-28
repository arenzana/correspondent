import Foundation

/// The list with arrivals slotted in by its order (newest published first,
/// as the server sends it), and every row the page refreshed. Rows already
/// shown keep their places: in Unread the page leaves out what was read
/// here, and rebuilding from it pushed those rows, the open one too, to the
/// end, under a second heading for their day. The web's placeArrivals.
public func placeArrivals(_ list: [Entry], page: [Entry], added: [Entry]) -> [Entry] {
    let latest = Dictionary(page.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
    var out = list.map { latest[$0.id] ?? $0 }
    func before(_ a: Entry, _ b: Entry) -> Bool {
        a.publishedAt != b.publishedAt ? a.publishedAt > b.publishedAt : (Int64(a.id) ?? 0) > (Int64(b.id) ?? 0)
    }
    for e in added {
        out.insert(e, at: out.firstIndex { before(e, $0) } ?? out.count)
    }
    return out
}
