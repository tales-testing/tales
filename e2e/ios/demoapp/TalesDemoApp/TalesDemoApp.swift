import SwiftUI

@main
struct TalesDemoApp: App {
    @StateObject private var auth = AuthStore()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(auth)
        }
    }
}

// MARK: - Auth store

final class AuthStore: ObservableObject {
    static let badEmail = "bad@example.com"

    @Published var isAuthenticated = false
    @Published var currentEmail = ""

    @Published var loginError = ""
    @Published var isLoggingIn = false

    @Published var registerError = ""
    @Published var isRegistering = false

    func signIn(email: String, password: String) {
        loginError = ""

        if email.isEmpty || !email.contains("@") {
            loginError = "Enter a valid email"
            return
        }

        if password.isEmpty {
            loginError = "Enter a password"
            return
        }

        if email.lowercased() == Self.badEmail {
            loginError = "Account locked. Contact support."
            return
        }

        isLoggingIn = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { [weak self] in
            guard let self = self else { return }
            self.isLoggingIn = false
            self.currentEmail = email
            self.isAuthenticated = true
        }
    }

    func register(email: String, password: String, repeatPassword: String, acceptTerms: Bool, acceptPrivacy: Bool) {
        registerError = ""

        if email.isEmpty || !email.contains("@") {
            registerError = "Enter a valid email"
            return
        }

        if !email.lowercased().hasSuffix("@example.com") {
            registerError = "Use an @example.com email"
            return
        }

        if password.count < 8 {
            registerError = "Password must be at least 8 characters"
            return
        }

        if password != repeatPassword {
            registerError = "Passwords do not match"
            return
        }

        if !acceptTerms {
            registerError = "You must accept the terms"
            return
        }

        if !acceptPrivacy {
            registerError = "You must accept the privacy policy"
            return
        }

        if email.lowercased() == Self.badEmail {
            registerError = "Account locked. Contact support."
            return
        }

        isRegistering = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { [weak self] in
            guard let self = self else { return }
            self.isRegistering = false
            self.currentEmail = email
            self.isAuthenticated = true
        }
    }

    func signOut() {
        isAuthenticated = false
        currentEmail = ""
        loginError = ""
        registerError = ""
    }
}

// MARK: - Feed data

struct FeedItem: Identifiable, Hashable {
    let id: Int
    let title: String
    let subtitle: String
    let body: String
}

enum DemoData {
    static let feedItems: [FeedItem] = (0..<50).map { i in
        FeedItem(
            id: i,
            title: "Lorem ipsum item #\(i)",
            subtitle: "Dolor sit amet entry \(i)",
            body: "Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
                "Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
                "This is body number \(i) used to exercise scroll and detail navigation in Tales E2E."
        )
    }
}

// MARK: - Root

struct RootView: View {
    @EnvironmentObject var auth: AuthStore

    var body: some View {
        Group {
            if auth.isAuthenticated {
                MainTabView()
            } else {
                NavigationStack {
                    WelcomeView()
                }
            }
        }
    }
}

// MARK: - Welcome

struct WelcomeView: View {
    var body: some View {
        VStack(spacing: 16) {
            Spacer()

            Text("Tales Demo")
                .font(.largeTitle.bold())
                .accessibilityIdentifier("welcome.title")

            Text("Welcome to the demo app")
                .foregroundStyle(.secondary)

            Spacer()

            NavigationLink {
                LoginView()
            } label: {
                Text("Sign In")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .accessibilityIdentifier("welcome.signin")

            NavigationLink {
                RegisterView()
            } label: {
                Text("Create Account")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("welcome.register")

            NavigationLink {
                ReproView()
            } label: {
                Text("Diagnostic")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("welcome.repro")

            NavigationLink {
                GestureView()
            } label: {
                Text("Gestures")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("welcome.gestures")
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
    }
}

// MARK: - Gestures

/// Playground exercising the gesture actions (long_press, double_tap,
/// swipe, scroll). Every gesture target is paired with a plain-text
/// status mirror so XCUITest can assert the gesture actually landed.
struct GestureView: View {
    @State private var longPressCount = 0
    @State private var doubleTapCount = 0
    @State private var swipeDirection = "none"

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                Text("Gestures")
                    .font(.title.bold())
                    .accessibilityIdentifier("gestures.screen")

                Text("Long press me")
                    .frame(maxWidth: .infinity)
                    .padding()
                    .background(Color.blue.opacity(0.15), in: RoundedRectangle(cornerRadius: 8))
                    .contentShape(Rectangle())
                    .onLongPressGesture { longPressCount += 1 }
                    .accessibilityIdentifier("gestures.longpress.target")
                Text("longpress=\(longPressCount)")
                    .monospaced()
                    .accessibilityIdentifier("gestures.status.longpress")

                Text("Double tap me")
                    .frame(maxWidth: .infinity)
                    .padding()
                    .background(Color.green.opacity(0.15), in: RoundedRectangle(cornerRadius: 8))
                    .contentShape(Rectangle())
                    .onTapGesture(count: 2) { doubleTapCount += 1 }
                    .accessibilityIdentifier("gestures.doubletap.target")
                Text("doubletap=\(doubleTapCount)")
                    .monospaced()
                    .accessibilityIdentifier("gestures.status.doubletap")

                Text("Swipe me")
                    .frame(maxWidth: .infinity, minHeight: 80)
                    .background(Color.orange.opacity(0.15), in: RoundedRectangle(cornerRadius: 8))
                    .contentShape(Rectangle())
                    .gesture(
                        DragGesture(minimumDistance: 20)
                            .onEnded { value in
                                let dx = value.translation.width
                                let dy = value.translation.height
                                if abs(dx) > abs(dy) {
                                    swipeDirection = dx > 0 ? "right" : "left"
                                } else {
                                    swipeDirection = dy > 0 ? "down" : "up"
                                }
                            }
                    )
                    .accessibilityIdentifier("gestures.swipe.target")
                Text("swipe=\(swipeDirection)")
                    .monospaced()
                    .accessibilityIdentifier("gestures.status.swipe")

                Divider()

                // Long list to exercise the scroll action. LazyVStack only
                // realizes rows as they approach the viewport, so a row far
                // down the list is genuinely absent from the accessibility
                // tree until a scroll brings it into range — making
                // wait_visible a real assertion that the scroll happened.
                Text("Scroll list")
                    .font(.headline)
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(0..<40) { index in
                        Text("Row \(index)")
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.vertical, 10)
                            .accessibilityIdentifier("gestures.row.\(index)")
                    }
                }
            }
            .padding(24)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
        .accessibilityIdentifier("gestures.scroll")
    }
}

// MARK: - Repro

/// Isolated playground for the iOS UI quirks that broke real-world
/// scenarios. Each control is paired with a status mirror whose plain
/// text reflects state, so XCUITest can assert without inferring from
/// SecureField bullets or KVO-only `value` properties.
struct ReproView: View {
    @State private var acceptTerms = false
    @State private var acceptPrivacy = false
    @State private var acceptMarketing = false

    @State private var password = ""
    @State private var confirmPassword = ""
    @State private var existingPassword = ""

    private var passwordsMatch: Bool {
        !confirmPassword.isEmpty && password == confirmPassword
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                Text("Diagnostic")
                    .font(.title.bold())
                    .accessibilityIdentifier("repro.screen")

                togglesSection
                Divider()
                secureFieldsSection
            }
            .padding(24)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
    }

    private var togglesSection: some View {
        VStack(alignment: .leading, spacing: 20) {
            // SwiftUI Toggle whose label embeds multiple Links — center
            // tap risks hitting one of the Links instead of the UISwitch.
            Toggle(isOn: $acceptTerms) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("I accept the terms of service.")
                    HStack(spacing: 12) {
                        Link("Sales terms", destination: URL(string: "https://example.com/sales")!)
                        Link("General terms", destination: URL(string: "https://example.com/terms")!)
                    }
                    .font(.caption)
                }
            }
            .accessibilityIdentifier("repro.toggle.accept_terms")

            Toggle(isOn: $acceptPrivacy) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("I acknowledge the privacy policy.")
                    Link("Read policy", destination: URL(string: "https://example.com/privacy")!)
                        .font(.caption)
                }
            }
            .accessibilityIdentifier("repro.toggle.accept_privacy")

            // Control: no interactive child in the label. Must always work
            // even when the other two regress — proves the bug is link-
            // related, not a general tap regression.
            Toggle(isOn: $acceptMarketing) {
                Text("I accept marketing communications.")
            }
            .accessibilityIdentifier("repro.toggle.accept_marketing")

            Group {
                Text("terms=\(acceptTerms ? "1" : "0")")
                    .accessibilityIdentifier("repro.status.terms")
                Text("privacy=\(acceptPrivacy ? "1" : "0")")
                    .accessibilityIdentifier("repro.status.privacy")
                Text("marketing=\(acceptMarketing ? "1" : "0")")
                    .accessibilityIdentifier("repro.status.marketing")
            }
            .monospaced()
        }
    }

    private var secureFieldsSection: some View {
        VStack(alignment: .leading, spacing: 16) {
            // .newPassword triggers the iOS "Use Strong Password" overlay
            // that intercepts soft-keyboard input on second focus.
            SecureField("Password", text: $password)
                .textContentType(.newPassword)
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("repro.password")

            SecureField("Confirm password", text: $confirmPassword)
                .textContentType(.newPassword)
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("repro.password_confirm")

            // Control: .password does not trigger the strong-password
            // overlay. Should always type the full string regardless of
            // focus order.
            SecureField("Existing password", text: $existingPassword)
                .textContentType(.password)
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("repro.existing_password")

            if !confirmPassword.isEmpty && !passwordsMatch {
                Text("Passwords do not match.")
                    .foregroundStyle(.red)
                    .accessibilityIdentifier("repro.mismatch_error")
            }

            Group {
                Text("password_len=\(password.count)")
                    .accessibilityIdentifier("repro.status.password_len")
                Text("confirm_len=\(confirmPassword.count)")
                    .accessibilityIdentifier("repro.status.confirm_len")
                Text("existing_len=\(existingPassword.count)")
                    .accessibilityIdentifier("repro.status.existing_len")
                Text("match=\(passwordsMatch ? "1" : "0")")
                    .accessibilityIdentifier("repro.status.match")
            }
            .monospaced()
        }
    }
}

// MARK: - Login

struct LoginView: View {
    @EnvironmentObject var auth: AuthStore
    @Environment(\.dismiss) private var dismiss

    @State private var email = ""
    @State private var password = ""

    var body: some View {
        VStack(spacing: 16) {
            Text("Login screen")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .accessibilityIdentifier("login.screen")

            Text("Sign in")
                .font(.title.bold())

            TextField("Email", text: $email)
                .textInputAutocapitalization(.never)
                .keyboardType(.emailAddress)
                .autocorrectionDisabled()
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("login.email")

            SecureField("Password", text: $password)
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("login.password")

            if !auth.loginError.isEmpty {
                Text(auth.loginError)
                    .foregroundStyle(.red)
                    .accessibilityIdentifier("login.error")
            }

            if auth.isLoggingIn {
                ProgressView("Signing in")
                    .accessibilityIdentifier("login.loading")
            }

            Button {
                auth.signIn(email: email, password: password)
            } label: {
                Text("Sign In")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .disabled(email.isEmpty || password.isEmpty || auth.isLoggingIn)
            .accessibilityIdentifier("login.submit")

            Button("Back") {
                dismiss()
            }
            .accessibilityIdentifier("login.back")

            Spacer()
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
    }
}

// MARK: - Register

struct RegisterView: View {
    @EnvironmentObject var auth: AuthStore
    @Environment(\.dismiss) private var dismiss

    @State private var email = ""
    @State private var password = ""
    @State private var repeatPassword = ""
    @State private var acceptTerms = false
    @State private var acceptPrivacy = false

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                Text("Register screen")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("register.screen")

                Text("Create account")
                    .font(.title.bold())

                TextField("Email", text: $email)
                    .textInputAutocapitalization(.never)
                    .keyboardType(.emailAddress)
                    .autocorrectionDisabled()
                    .textFieldStyle(.roundedBorder)
                    .accessibilityIdentifier("register.email")

                // Reproduces the production signup quirk: .newPassword content
                // type makes iOS show the "Use Strong Password" QuickType banner,
                // which intercepts soft-keyboard keystrokes on second focus.
                SecureField("Password", text: $password)
                    .textContentType(.newPassword)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityIdentifier("register.password")

                SecureField("Repeat password", text: $repeatPassword)
                    .textContentType(.newPassword)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityIdentifier("register.repeat_password")

                // Real SwiftUI Toggle whose label embeds a Link.
                // XCUITest hit-tests at the toggle center, which lands on the
                // Link inside the label rather than the UISwitch on the right.
                Toggle(isOn: $acceptTerms) {
                    HStack(spacing: 4) {
                        Text("I accept the")
                        Link("terms and conditions", destination: URL(string: "https://example.com/terms")!)
                    }
                }
                .accessibilityIdentifier("register.accept_terms")

                Toggle(isOn: $acceptPrivacy) {
                    HStack(spacing: 4) {
                        Text("I accept the")
                        Link("privacy policy", destination: URL(string: "https://example.com/privacy")!)
                    }
                }
                .accessibilityIdentifier("register.accept_privacy")

                if !auth.registerError.isEmpty {
                    Text(auth.registerError)
                        .foregroundStyle(.red)
                        .accessibilityIdentifier("register.error")
                }

                if auth.isRegistering {
                    ProgressView("Creating account")
                        .accessibilityIdentifier("register.loading")
                }

                Button {
                    auth.register(
                        email: email,
                        password: password,
                        repeatPassword: repeatPassword,
                        acceptTerms: acceptTerms,
                        acceptPrivacy: acceptPrivacy
                    )
                } label: {
                    Text("Register")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .disabled(email.isEmpty || password.isEmpty || auth.isRegistering)
                .accessibilityIdentifier("register.submit")

                Button("Back") {
                    dismiss()
                }
                .accessibilityIdentifier("register.back")
            }
            .padding(24)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
    }
}

// MARK: - Main tabbar

enum MainTab: Hashable {
    case feed
    case search
    case profile
}

struct MainTabView: View {
    @State private var selection: MainTab = .feed

    var body: some View {
        TabView(selection: $selection) {
            NavigationStack {
                FeedView()
            }
            .tabItem {
                Label("Feed", systemImage: "list.bullet")
                    .accessibilityIdentifier("tabbar.feed")
            }
            .tag(MainTab.feed)

            NavigationStack {
                SearchView()
            }
            .tabItem {
                Label("Search", systemImage: "magnifyingglass")
                    .accessibilityIdentifier("tabbar.search")
            }
            .tag(MainTab.search)

            NavigationStack {
                ProfileView()
            }
            .tabItem {
                Label("Profile", systemImage: "person.crop.circle")
                    .accessibilityIdentifier("tabbar.profile")
            }
            .tag(MainTab.profile)
        }
    }
}

// MARK: - Feed tab

struct FeedView: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Feed")
                .font(.title.bold())
                .padding(.horizontal, 16)
                .padding(.top, 12)
                .accessibilityIdentifier("feed.screen")

            List {
                ForEach(DemoData.feedItems) { item in
                    NavigationLink(value: item) {
                        FeedRow(item: item)
                    }
                    .accessibilityElement(children: .contain)
                    .accessibilityIdentifier("feed.item.\(item.id)")
                }
            }
            .listStyle(.plain)
            .accessibilityIdentifier("feed.list")
        }
        .navigationDestination(for: FeedItem.self) { item in
            FeedDetailView(item: item)
        }
    }
}

struct FeedRow: View {
    let item: FeedItem

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(item.title)
                .font(.headline)
                .accessibilityIdentifier("feed.item.\(item.id).title")

            Text(item.subtitle)
                .font(.caption)
                .foregroundStyle(.secondary)
                .accessibilityIdentifier("feed.item.\(item.id).subtitle")
        }
        .padding(.vertical, 4)
    }
}

struct FeedDetailView: View {
    let item: FeedItem
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Text(item.title)
                    .font(.title.bold())
                    .accessibilityIdentifier("feed.detail.title")

                Text(item.body)
                    .accessibilityIdentifier("feed.detail.body")

                Button("Back") {
                    dismiss()
                }
                .buttonStyle(.bordered)
                .accessibilityIdentifier("feed.detail.back")
            }
            .padding(24)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityIdentifier("feed.detail.screen")
    }
}

// MARK: - Search tab

struct SearchView: View {
    @State private var query = ""
    @FocusState private var fieldFocused: Bool

    var filtered: [FeedItem] {
        guard !query.isEmpty else { return [] }
        let needle = query.lowercased()
        return DemoData.feedItems.filter {
            $0.title.lowercased().contains(needle) ||
                $0.subtitle.lowercased().contains(needle)
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Search")
                .font(.title.bold())
                .accessibilityIdentifier("search.screen")
                .contentShape(Rectangle())
                .onTapGesture { fieldFocused = false }

            TextField("Type to search", text: $query)
                .textFieldStyle(.roundedBorder)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .focused($fieldFocused)
                .submitLabel(.done)
                .onSubmit { fieldFocused = false }
                .accessibilityIdentifier("search.field")

            if query.isEmpty {
                Text("Type to search")
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("search.empty")
            } else if filtered.isEmpty {
                Text("No results")
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("search.empty")
            } else {
                Text("\(filtered.count) results")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("search.results.count")

                List {
                    ForEach(Array(filtered.enumerated()), id: \.element.id) { idx, item in
                        Text(item.title)
                            .accessibilityIdentifier("search.item.\(idx).title")
                    }
                }
                .listStyle(.plain)
            }

            Spacer()
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - Profile tab

struct ProfileView: View {
    @EnvironmentObject var auth: AuthStore
    @State private var notificationsEnabled = true

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            Text("Profile")
                .font(.title.bold())
                .accessibilityIdentifier("profile.screen")

            VStack(alignment: .leading, spacing: 4) {
                Text("Signed in as")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text(auth.currentEmail)
                    .font(.headline)
                    .accessibilityIdentifier("profile.email")
            }

            Button {
                notificationsEnabled.toggle()
            } label: {
                HStack {
                    Text("Notifications")
                        .foregroundStyle(.primary)
                    Spacer()
                    Image(systemName: notificationsEnabled ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(notificationsEnabled ? Color.accentColor : Color.secondary)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("profile.notifications")
            .accessibilityValue(notificationsEnabled ? "1" : "0")

            Text(notificationsEnabled ? "On" : "Off")
                .foregroundStyle(.secondary)
                .accessibilityIdentifier("profile.notifications.label")

            Spacer()

            Button {
                auth.signOut()
            } label: {
                Text("Sign out")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .tint(.red)
            .accessibilityIdentifier("profile.logout")
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}
