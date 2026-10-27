import SwiftUI
import CorrespondentKit

/// Add a feed, as the web's dialog (ui/src/components/AddFeed.svelte): an
/// address finds the site's feeds; a site with none can be read by your AI.
struct AddFeedView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss
    @State private var address = ""
    @State private var found: [Candidate] = []
    @State private var chosen: Candidate?
    @State private var folder = ""
    @State private var busy = false
    @State private var asked = false
    @State private var message = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Add a feed").font(Typeface.read.font(23, weight: 560, opsz: 36)).foregroundStyle(theme.ink)
                Spacer()
                Button("Cancel") { dismiss() }.tint(theme.accent).font(Typeface.ui.font(15, weight: 520))
            }
            .padding(.horizontal, 20).padding(.top, 20).padding(.bottom, 14)

            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    HStack(spacing: 8) {
                        TextField("A site, page or feed address", text: $address)
                            .focused($focused)
                            .autocorrectionDisabled()
                            #if os(iOS)
                            .keyboardType(.URL).textInputAutocapitalization(.never)
                            #endif
                            .onSubmit { Task { await find() } }
                            .modifier(FieldStyle())
                        Button { Task { await find() } } label: {
                            Text(busy ? "Finding…" : "Find").font(Typeface.ui.font(15, weight: 600)).foregroundStyle(.white)
                                .padding(.horizontal, 16).frame(height: 42)
                                .background(RoundedRectangle(cornerRadius: 6).fill(theme.accent))
                        }
                        .buttonStyle(.plain).disabled(busy || address.trimmingCharacters(in: .whitespaces).isEmpty)
                    }

                    if !message.isEmpty {
                        Text(message).font(Typeface.ui.font(14)).foregroundStyle(theme.slate)
                    }
                    if asked && found.isEmpty && !busy {
                        VStack(alignment: .leading, spacing: 8) {
                            Text("No feed found there.").font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
                            Text("If the page lists articles, your AI can write a parser for it, once. Checking for new articles never uses AI.")
                                .font(Typeface.ui.font(14)).foregroundStyle(theme.slate)
                            Button("Read the page with my AI") { Task { await find(ai: true) } }.buttonStyle(QuietButton())
                        }
                    }

                    ForEach(found) { c in candidate(c) }

                    if chosen != nil {
                        VStack(alignment: .leading, spacing: 8) {
                            Text("Folder").font(Typeface.ui.font(13.5, weight: 600)).foregroundStyle(theme.ink)
                            TextField("None", text: $folder).modifier(FieldStyle())
                            if !app.tree.folders.isEmpty {
                                ScrollView(.horizontal, showsIndicators: false) {
                                    HStack(spacing: 6) {
                                        ForEach(app.tree.folders) { f in
                                            Button(f.name) { folder = f.name }
                                                .font(Typeface.ui.font(13.5, weight: folder == f.name ? 600 : 450))
                                                .foregroundStyle(folder == f.name ? theme.accent : theme.ink)
                                                .padding(.horizontal, 10).frame(height: 30)
                                                .background(Capsule().stroke(folder == f.name ? theme.accent : theme.rule))
                                                .buttonStyle(.plain)
                                        }
                                    }
                                }
                            }
                        }
                        Button {
                            Task {
                                busy = true
                                if let c = chosen, await app.subscribe(c, folder: folder.trimmingCharacters(in: .whitespaces)) { dismiss() }
                                busy = false
                            }
                        } label: {
                            Text("Subscribe").font(Typeface.ui.font(16, weight: 600)).foregroundStyle(.white)
                                .frame(maxWidth: .infinity).frame(height: 44)
                                .background(RoundedRectangle(cornerRadius: 6).fill(theme.accent))
                        }
                        .buttonStyle(.plain).disabled(busy)
                    }
                }
                .padding(.horizontal, 20).padding(.bottom, 24)
            }
        }
        .background(theme.sheet)
        .onAppear { focused = true }
    }

    private func candidate(_ c: Candidate) -> some View {
        let on = chosen == c
        return Button { chosen = c } label: {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: on ? "largecircle.fill.circle" : "circle").foregroundStyle(on ? theme.accent : theme.ring)
                    .padding(.top, 2)
                VStack(alignment: .leading, spacing: 4) {
                    Text(c.title.isEmpty ? c.url : c.title).font(Typeface.read.font(17, weight: 560)).foregroundStyle(theme.ink)
                    Text(c.page ? "A page read without a feed" : "\(c.itemCount) articles")
                        .font(Typeface.ui.font(13)).foregroundStyle(theme.slate)
                    if !c.description.isEmpty {
                        Text(c.description).font(Typeface.ui.font(13.5)).foregroundStyle(theme.slate).lineLimit(2)
                    }
                    ForEach(c.sample.prefix(3), id: \.self) { t in
                        Text("· " + t).font(Typeface.ui.font(13)).foregroundStyle(theme.slate).lineLimit(1)
                    }
                }
                Spacer()
            }
            .padding(14)
            .background(RoundedRectangle(cornerRadius: 10).stroke(on ? theme.accent : theme.rule, lineWidth: on ? 2 : 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func find(ai: Bool = false) async {
        let a = address.trimmingCharacters(in: .whitespaces)
        guard !a.isEmpty, let client = app.client else { return }
        busy = true
        message = ai ? "Your AI is reading the page. This can take a minute." : ""
        defer { busy = false }
        do {
            found = try await client.discover(a, ai: ai)
            chosen = found.count == 1 ? found[0] : nil
            message = ""
        } catch {
            found = []
            message = error.localizedDescription
        }
        asked = !ai
    }
}
