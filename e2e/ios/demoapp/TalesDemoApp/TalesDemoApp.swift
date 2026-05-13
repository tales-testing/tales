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

    func register(email: String, password: String, repeatPassword: String, acceptTerms: Bool) {
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
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
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

                SecureField("Password", text: $password)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityIdentifier("register.password")

                SecureField("Repeat password", text: $repeatPassword)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityIdentifier("register.repeat_password")

                Button {
                    acceptTerms.toggle()
                } label: {
                    HStack(spacing: 8) {
                        Image(systemName: acceptTerms ? "checkmark.square.fill" : "square")
                            .foregroundStyle(acceptTerms ? Color.accentColor : Color.secondary)
                        Text("I accept the terms and conditions")
                            .foregroundStyle(.primary)
                            .multilineTextAlignment(.leading)
                        Spacer()
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("register.accept_terms")
                .accessibilityValue(acceptTerms ? "1" : "0")

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
                        acceptTerms: acceptTerms
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
