/// FileHost listener

function ListenerUI(mode_create)
{
    let labelBind = form.create_label("Bind address:");
    let comboHost = form.create_combo();
    comboHost.setEnabled(mode_create);
    comboHost.clear();
    let addrs = ax.interfaces();
    for (let item of addrs) { comboHost.addItem(item); }

    let spinPort = form.create_spin();
    spinPort.setRange(1, 65535);
    spinPort.setValue(18080);
    spinPort.setEnabled(mode_create);

    let labelRoot = form.create_label("Root directory:");
    let textRoot = form.create_textline("/opt/adaptix/teamserver/data/filehost/files");

    let labelToken = form.create_label("URL path token:");
    let textToken = form.create_textline(ax.random_string(32, "hex"));
    let btnToken = form.create_button("Generate");
    form.connect(btnToken, "clicked", function() { textToken.setText(ax.random_string(32, "hex")); });

    let labelDomain = form.create_label("External domain (blank = local only):");
    let textDomain = form.create_textline("files.sweeper.channel");
    textDomain.setEnabled(mode_create);

    let labelZone = form.create_label("Cloudflare zone:");
    let textZone = form.create_textline("sweeper.channel");
    textZone.setEnabled(mode_create);

    let labelTunnel = form.create_label("Tunnel ID:");
    let textTunnel = form.create_textline("21d02c65-e0c3-4b3b-8457-5ace81f88541");
    textTunnel.setEnabled(mode_create);

    let labelTokenFile = form.create_label("CF token file (0600, recommended):");
    let textTokenFile = form.create_textline("/opt/adaptix/teamserver/data/cf_token");
    textTokenFile.setEnabled(mode_create);

    let labelTokenInline = form.create_label("CF token (optional; stored in listener config):");
    let textTokenInline = form.create_textline("");
    textTokenInline.setEnabled(mode_create);

    let checkAutoDNS = form.create_check("Auto-create DNS record");
    checkAutoDNS.setChecked(true);
    checkAutoDNS.setEnabled(mode_create);

    let checkTunnel = form.create_check("Update tunnel ingress");
    checkTunnel.setChecked(true);
    checkTunnel.setEnabled(mode_create);

    let labelMax = form.create_label("Max file size (MB, 0 = unlimited):");
    let spinMax = form.create_spin();
    spinMax.setRange(0, 65536);
    spinMax.setValue(2048);

    let labelExpiry = form.create_label("Link expiry (minutes, 0 = never):");
    let spinExpiry = form.create_spin();
    spinExpiry.setRange(0, 43200);
    spinExpiry.setValue(0);

    let labelAllow = form.create_label("IP allowlist (CIDR, optional):");
    let listAllow = form.create_list();
    listAllow.setButtonsEnabled(true);

    let labelError = form.create_label("404 page:");
    let textError = form.create_textmulti("");

    let layout = form.create_gridlayout();
    layout.addWidget(labelBind,        0, 0, 1, 1);
    layout.addWidget(comboHost,        0, 1, 1, 1);
    layout.addWidget(spinPort,         0, 2, 1, 1);
    layout.addWidget(labelRoot,        1, 0, 1, 1);
    layout.addWidget(textRoot,         1, 1, 1, 2);
    layout.addWidget(labelToken,       2, 0, 1, 1);
    layout.addWidget(textToken,        2, 1, 1, 1);
    layout.addWidget(btnToken,         2, 2, 1, 1);
    layout.addWidget(labelDomain,      3, 0, 1, 1);
    layout.addWidget(textDomain,       3, 1, 1, 2);
    layout.addWidget(labelZone,        4, 0, 1, 1);
    layout.addWidget(textZone,         4, 1, 1, 2);
    layout.addWidget(labelTunnel,      5, 0, 1, 1);
    layout.addWidget(textTunnel,       5, 1, 1, 2);
    layout.addWidget(labelTokenFile,   6, 0, 1, 1);
    layout.addWidget(textTokenFile,    6, 1, 1, 2);
    layout.addWidget(labelTokenInline, 7, 0, 1, 1);
    layout.addWidget(textTokenInline,  7, 1, 1, 2);
    layout.addWidget(checkAutoDNS,     8, 0, 1, 2);
    layout.addWidget(checkTunnel,      8, 2, 1, 1);
    layout.addWidget(labelMax,         9, 0, 1, 1);
    layout.addWidget(spinMax,          9, 1, 1, 1);
    layout.addWidget(labelExpiry,     10, 0, 1, 1);
    layout.addWidget(spinExpiry,      10, 1, 1, 1);
    layout.addWidget(labelAllow,      11, 0, 1, 1);
    layout.addWidget(listAllow,       11, 1, 1, 2);
    layout.addWidget(labelError,      12, 0, 1, 1);
    layout.addWidget(textError,       12, 1, 1, 2);

    let panel = form.create_panel();
    panel.setLayout(layout);

    let container = form.create_container();
    container.put("host_bind",             comboHost);
    container.put("port_bind",             spinPort);
    container.put("root_dir",              textRoot);
    container.put("path_token",            textToken);
    container.put("external_domain",       textDomain);
    container.put("cf_zone",               textZone);
    container.put("cf_tunnel_id",          textTunnel);
    container.put("cf_token_file",         textTokenFile);
    container.put("cf_api_token",          textTokenInline);
    container.put("auto_create_dns",       checkAutoDNS);
    container.put("update_tunnel_ingress", checkTunnel);
    container.put("max_file_mb",           spinMax);
    container.put("link_expiry_minutes",   spinExpiry);
    container.put("ip_allowlist",          listAllow);
    container.put("page_error",            textError);

    return {
        ui_panel: panel,
        ui_container: container,
        ui_height: 720,
        ui_width: 780
    }
}
