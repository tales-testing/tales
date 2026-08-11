package org.taleslabs.tales.demo

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp

/** The fixed feed the scenarios navigate. */
private val feedItems: List<Pair<String, String>> = (0 until 50).map { index ->
    "Item $index" to "Subtitle for item $index"
}

@Composable
fun FeedScreen(onOpen: (Int) -> Unit, onSearch: () -> Unit, onProfile: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp)
            .testTag("feed.screen"),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = onSearch, modifier = Modifier.testTag("feed.search")) {
                Text("Search")
            }

            Button(onClick = onProfile, modifier = Modifier.testTag("feed.profile")) {
                Text("Profile")
            }
        }

        LazyColumn(modifier = Modifier.testTag("feed.list")) {
            items(feedItems.indices.toList()) { index ->
                val (title, subtitle) = feedItems[index]

                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { onOpen(index) }
                        .padding(vertical = 8.dp)
                        .testTag("feed.item.$index"),
                ) {
                    Text(title, modifier = Modifier.testTag("feed.item.$index.title"))
                    Text(subtitle, modifier = Modifier.testTag("feed.item.$index.subtitle"))
                }
            }
        }
    }
}

@Composable
fun FeedDetailScreen(index: Int, onBack: () -> Unit) {
    val (title, subtitle) = feedItems.getOrElse(index) { "Unknown" to "" }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp)
            .testTag("feed.detail.screen"),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        TextButton(onClick = onBack, modifier = Modifier.testTag("feed.detail.back")) {
            Text("Back")
        }

        Text(title, modifier = Modifier.testTag("feed.detail.title"))
        Text(subtitle, modifier = Modifier.testTag("feed.detail.body"))
    }
}

@Composable
fun SearchScreen(onBack: () -> Unit) {
    var query by remember { mutableStateOf("") }

    val results = if (query.isBlank()) {
        emptyList()
    } else {
        feedItems.filter { it.first.contains(query, ignoreCase = true) }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp)
            .testTag("search.screen"),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        TextButton(onClick = onBack, modifier = Modifier.testTag("search.back")) {
            Text("Back")
        }

        OutlinedTextField(
            value = query,
            onValueChange = { query = it },
            label = { Text("Search") },
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .testTag("search.field"),
        )

        Text("results=${results.size}", modifier = Modifier.testTag("search.results.count"))

        if (results.isEmpty()) {
            Text("No results", modifier = Modifier.testTag("search.empty"))
        }

        LazyColumn {
            items(results.indices.toList()) { idx ->
                Text(results[idx].first, modifier = Modifier.testTag("search.item.$idx.title"))
            }
        }
    }
}

@Composable
fun ProfileScreen(email: String, onBack: () -> Unit, onLogout: () -> Unit) {
    var notifications by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp)
            .testTag("profile.screen"),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        TextButton(onClick = onBack, modifier = Modifier.testTag("profile.back")) {
            Text("Back")
        }

        Text(email, modifier = Modifier.testTag("profile.email"))

        Row(verticalAlignment = Alignment.CenterVertically) {
            // The label carries the state as text as well as the switch
            // carrying it as a checked flag, so a scenario can assert
            // either way — `expect { text }` or `expect { value }`.
            Text(
                "notifications=${if (notifications) "on" else "off"}",
                modifier = Modifier.testTag("profile.notifications.label"),
            )

            Switch(
                checked = notifications,
                onCheckedChange = { notifications = it },
                modifier = Modifier.testTag("profile.notifications"),
            )
        }

        Button(onClick = onLogout, modifier = Modifier.testTag("profile.logout")) {
            Text("Log out")
        }
    }
}
