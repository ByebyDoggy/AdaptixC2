#include <UI/Dialogs/DialogAutomation.h>
#include <UI/Dialogs/DialogAutomationRule.h>
#include <Client/AuthProfile.h>
#include <Client/Requestor.h>
#include <QPointer>
#include <QDialog>
#include <QDialogButtonBox>

static QTableView* makeTable(QWidget* parent, QStandardItemModel* model, const QStringList &headers)
{
    model->setColumnCount(headers.size());
    for (int i = 0; i < headers.size(); ++i)
        model->setHorizontalHeaderItem(i, new QStandardItem(headers.at(i)));

    auto* table = new QTableView(parent);
    table->setModel(model);
    table->setShowGrid(false);
    table->setWordWrap(false);
    table->setSelectionBehavior(QAbstractItemView::SelectRows);
    table->setSelectionMode(QAbstractItemView::SingleSelection);
    table->setEditTriggers(QAbstractItemView::NoEditTriggers);
    table->setAlternatingRowColors(true);
    table->verticalHeader()->setVisible(false);
    table->horizontalHeader()->setSectionResizeMode(QHeaderView::ResizeToContents);
    table->horizontalHeader()->setStretchLastSection(true);
    return table;
}

static QStandardItem* makeCell(const QString &text)
{
    auto* item = new QStandardItem(text);
    item->setFlags(item->flags() & ~Qt::ItemIsEditable);
    return item;
}

DialogAutomation::DialogAutomation(AuthProfile* authProfile, QWidget* parent) : QWidget(parent), profile(authProfile)
{
    createUI();

    connect(refreshButton, &QPushButton::clicked, this, &DialogAutomation::onRefresh);
    connect(closeButton,   &QPushButton::clicked, this, &QWidget::close);
    connect(rulesModel,    &QStandardItemModel::itemChanged, this, &DialogAutomation::onItemChanged);
    connect(navList,       &QListWidget::currentRowChanged, this, [this](int row) { stack->setCurrentIndex(row); });

    if (!profile) {
        setStatus("No active project.", true);
        return;
    }

    onRefresh();
    onRefreshTemplates();
    onRefreshScripts();
}

DialogAutomation::~DialogAutomation() = default;

void DialogAutomation::createUI()
{
    this->setWindowTitle("Automation / AutoExec");
    this->resize(1100, 680);
    this->setProperty("Main", "base");

    navList = new QListWidget(this);
    navList->setFixedWidth(180);
    navList->addItem("Rules");
    navList->addItem("ARTEX templates");
    navList->addItem("Notification templates");
    navList->addItem("Notification channels");
    navList->addItem("Shell script library");
    navList->setCurrentRow(0);

    stack = new QStackedWidget(this);
    stack->addWidget(createRulesPage());
    stack->addWidget(createArtexPage());
    stack->addWidget(createNotifyPage());
    stack->addWidget(createChannelsPage());
    stack->addWidget(createScriptsPage());

    statusLabel = new QLabel("", this);
    statusLabel->setWordWrap(true);
    statusLabel->setTextInteractionFlags(Qt::TextSelectableByMouse);

    refreshButton = new QPushButton("Refresh", this);
    closeButton   = new QPushButton("Close", this);

    auto* bottom = new QHBoxLayout();
    bottom->addWidget(statusLabel, 1);
    bottom->addWidget(refreshButton);
    bottom->addWidget(closeButton);

    auto* layout = new QVBoxLayout(this);
    layout->setContentsMargins(10, 10, 10, 10);
    auto* body = new QHBoxLayout();
    body->addWidget(navList);
    body->addWidget(stack, 1);
    layout->addLayout(body, 1);
    layout->addLayout(bottom);
    this->setLayout(layout);
}

QWidget* DialogAutomation::createRulesPage()
{
    auto* page = new QWidget(this);
    rulesModel = new QStandardItemModel(page);
    rulesView = makeTable(page, rulesModel, {"ID", "名称", "触发", "动作", "顺序", "启用", "最近触发"});
    // The name is the only free-text column; let it take the slack instead of
    // squeezing every column to a few characters.
    rulesView->horizontalHeader()->setStretchLastSection(false);
    rulesView->horizontalHeader()->setSectionResizeMode(1, QHeaderView::Stretch);
    rulesView->horizontalHeader()->setSectionResizeMode(6, QHeaderView::ResizeToContents);

    editRuleButton = new QPushButton("编辑配置...", page);

    connect(rulesView,      &QTableView::doubleClicked, this, [this](const QModelIndex &) { onEditRule(); });
    connect(editRuleButton, &QPushButton::clicked,      this, &DialogAutomation::onEditRule);

    auto* hint = new QLabel("双击规则或点击“编辑配置”修改单条规则的动作、模板与通知设置。规则本身在 profile.yaml 中定义。", page);
    hint->setWordWrap(true);

    auto* layout = new QVBoxLayout(page);
    layout->addWidget(rulesView, 1);
    layout->addWidget(hint);
    auto* buttons = new QHBoxLayout();
    buttons->addWidget(editRuleButton);
    buttons->addStretch(1);
    layout->addLayout(buttons);
    return page;
}

QWidget* DialogAutomation::createArtexPage()
{
    auto* page = new QWidget(this);
    artexModel = new QStandardItemModel(page);
    artexView = makeTable(page, artexModel, {"ID", "名称", "Worker", "Timeout", "去重", "冷却"});

    auto* newButton    = new QPushButton("新建...", page);
    auto* editButton   = new QPushButton("编辑...", page);
    auto* deleteButton = new QPushButton("删除", page);

    connect(newButton,    &QPushButton::clicked, this, &DialogAutomation::onNewArtexTemplate);
    connect(editButton,   &QPushButton::clicked, this, &DialogAutomation::onEditArtexTemplate);
    connect(deleteButton, &QPushButton::clicked, this, &DialogAutomation::onDeleteArtexTemplate);
    connect(artexView, &QTableView::doubleClicked, this, [this](const QModelIndex &) { onEditArtexTemplate(); });

    auto* hint = new QLabel("ARTEX 任务模板：提示词与任务参数（并发/超时/去重/冷却）。规则引用模板 id，可建多个复用。占位符：{{AGENT_ID}} {{AGENT_HOST}} {{AGENT_USER}} {{AGENT_OS}} {{AGENT_LISTENER}} {{AGENT_INTERNAL_IP}} {{AGENT_EXTERNAL_IP}} {{AGENT_DOMAIN}} {{AGENT_PROCESS}}", page);
    hint->setWordWrap(true);

    auto* layout = new QVBoxLayout(page);
    layout->addWidget(artexView, 1);
    layout->addWidget(hint);
    auto* buttons = new QHBoxLayout();
    buttons->addWidget(newButton);
    buttons->addWidget(editButton);
    buttons->addWidget(deleteButton);
    buttons->addStretch(1);
    layout->addLayout(buttons);
    return page;
}

QWidget* DialogAutomation::createNotifyPage()
{
    auto* page = new QWidget(this);
    notifyModel = new QStandardItemModel(page);
    notifyView = makeTable(page, notifyModel, {"ID", "名称", "标题", "正文预览"});

    auto* newButton    = new QPushButton("新建...", page);
    auto* editButton   = new QPushButton("编辑...", page);
    auto* deleteButton = new QPushButton("删除", page);

    connect(newButton,    &QPushButton::clicked, this, &DialogAutomation::onNewNotifyTemplate);
    connect(editButton,   &QPushButton::clicked, this, &DialogAutomation::onEditNotifyTemplate);
    connect(deleteButton, &QPushButton::clicked, this, &DialogAutomation::onDeleteNotifyTemplate);
    connect(notifyView, &QTableView::doubleClicked, this, [this](const QModelIndex &) { onEditNotifyTemplate(); });

    auto* hint = new QLabel("通知模板：标题与正文，支持 {{AGENT_*}} 占位符。规则选择通知渠道时引用这里的模板。", page);
    hint->setWordWrap(true);

    auto* layout = new QVBoxLayout(page);
    layout->addWidget(notifyView, 1);
    layout->addWidget(hint);
    auto* buttons = new QHBoxLayout();
    buttons->addWidget(newButton);
    buttons->addWidget(editButton);
    buttons->addWidget(deleteButton);
    buttons->addStretch(1);
    layout->addLayout(buttons);
    return page;
}

QWidget* DialogAutomation::createChannelsPage()
{
    auto* page = new QWidget(this);
    channelModel = new QStandardItemModel(page);
    channelView = makeTable(page, channelModel, {"ID", "名称", "已配置", "凭据（掩码）"});

    auto* configureButton = new QPushButton("配置凭据...", page);
    connect(configureButton, &QPushButton::clicked, this, &DialogAutomation::onConfigureChannel);
    connect(channelView, &QTableView::doubleClicked, this, [this](const QModelIndex &) { onConfigureChannel(); });

    auto* hint = new QLabel("通知渠道由服务端实现。凭据只保存在 teamserver，客户端仅显示掩码；留空表示不修改。", page);
    hint->setWordWrap(true);

    auto* layout = new QVBoxLayout(page);
    layout->addWidget(channelView, 1);
    layout->addWidget(hint);
    auto* buttons = new QHBoxLayout();
    buttons->addWidget(configureButton);
    buttons->addStretch(1);
    layout->addLayout(buttons);
    return page;
}

QWidget* DialogAutomation::createScriptsPage()
{
    auto* page = new QWidget(this);
    scriptModel = new QStandardItemModel(page);
    scriptView = makeTable(page, scriptModel, {"ID", "名称", "适用系统", "破坏性", "说明"});

    auto* hint = new QLabel("Shell 脚本库：服务端托管、下发到目标机执行（与 AxScript 的 .axs 编排脚本不同）。\n"
                            "执行入口在会话表：右键某个 agent → Scripts → 选择脚本，无需输入 agent id。", page);
    hint->setWordWrap(true);

    auto* layout = new QVBoxLayout(page);
    layout->addWidget(scriptView, 1);
    layout->addWidget(hint);
    return page;
}

void DialogAutomation::setStatus(const QString &text, bool error)
{
    statusLabel->setText(text);
    statusLabel->setStyleSheet(error ? "color:#e05a5a;" : "color:#5fd08a;");
}

void DialogAutomation::applyRules(const QJsonArray &rules, const QJsonArray &channels)
{
    rulesData = rules;
    if (!channels.isEmpty())
        channelsData = channels;

    updating = true;
    rulesModel->removeRows(0, rulesModel->rowCount());

    for (const QJsonValue &value : rules) {
        const QJsonObject rule = value.toObject();
        const QJsonObject actions = rule.value("actions").toObject();

        QStringList actionParts;
        const int commandCount = actions.value("commands").toInt();
        if (commandCount > 0)
            actionParts << QString("cmd×%1").arg(commandCount);
        if (!actions.value("artex_template").toString().isEmpty())
            actionParts << QString("artex:%1").arg(actions.value("artex_template").toString());
        else if (actions.value("artex").toBool())
            actionParts << "artex";
        if (actions.value("notify").toBool())
            actionParts << QString("notify:%1").arg(actions.value("notify_channel").toString());
        if (!actions.value("script").toString().isEmpty())
            actionParts << QString("script:%1").arg(actions.value("script").toString());
        if (actions.value("persist").toBool())
            actionParts << "persist";

        auto* idItem   = makeCell(rule.value("id").toString());
        auto* nameItem = makeCell(rule.value("name").toString());
        auto* trigItem = makeCell(rule.value("trigger_name").toString());
        auto* actItem  = makeCell(actionParts.isEmpty() ? "—" : actionParts.join(", "));
        auto* ordItem  = makeCell(QString::number(rule.value("order").toInt()));

        auto* checkItem = new QStandardItem();
        checkItem->setFlags(Qt::ItemIsEnabled | Qt::ItemIsSelectable | Qt::ItemIsUserCheckable);
        checkItem->setCheckState(rule.value("enabled").toBool() ? Qt::Checked : Qt::Unchecked);
        checkItem->setData(rule.value("id").toString(), Qt::UserRole);

        const qint64 lastRun = rule.value("last_run").toVariant().toLongLong();
        QString lastText = "未触发";
        if (lastRun > 0) {
            lastText = QDateTime::fromSecsSinceEpoch(lastRun).toString("MM-dd HH:mm");
            const QString status = rule.value("last_status").toString();
            if (!status.isEmpty())
                lastText += " " + status;
        }
        auto* lastItem = makeCell(lastText);
        if (rule.value("last_status").toString() == "error")
            lastItem->setForeground(QColor("#e05a5a"));
        else if (rule.value("last_status").toString() == "partial")
            lastItem->setForeground(QColor("#e0a75a"));

        QList<QStandardItem*> rowItems;
        rowItems << idItem << nameItem << trigItem << actItem << ordItem << checkItem << lastItem;
        rulesModel->appendRow(rowItems);
    }

    updating = false;
}

void DialogAutomation::applyTemplates(const QJsonObject &payload)
{
    artexTemplatesData  = payload.value("artex").toArray();
    notifyTemplatesData = payload.value("notify").toArray();

    artexModel->removeRows(0, artexModel->rowCount());
    for (const QJsonValue &value : artexTemplatesData) {
        const QJsonObject tpl = value.toObject();
        QList<QStandardItem*> rowItems;
        rowItems << makeCell(tpl.value("id").toString())
                 << makeCell(tpl.value("name").toString())
                 << makeCell(QString::number(tpl.value("worker_count").toInt()))
                 << makeCell(QString::number(tpl.value("timeout_seconds").toInt()))
                 << makeCell(tpl.value("dedup_by").toString())
                 << makeCell(QString::number(tpl.value("cooldown_seconds").toInt()));
        artexModel->appendRow(rowItems);
    }

    notifyModel->removeRows(0, notifyModel->rowCount());
    for (const QJsonValue &value : notifyTemplatesData) {
        const QJsonObject tpl = value.toObject();
        QString bodyPreview = tpl.value("body").toString().simplified();
        if (bodyPreview.size() > 80)
            bodyPreview = bodyPreview.left(80) + "…";

        QList<QStandardItem*> rowItems;
        rowItems << makeCell(tpl.value("id").toString())
                 << makeCell(tpl.value("name").toString())
                 << makeCell(tpl.value("title").toString())
                 << makeCell(bodyPreview);
        notifyModel->appendRow(rowItems);
    }
}

void DialogAutomation::applyChannels(const QJsonArray &channels)
{
    channelsData = channels;

    channelModel->removeRows(0, channelModel->rowCount());
    for (const QJsonValue &value : channels) {
        const QJsonObject channel = value.toObject();
        const bool configured = channel.value("configured").toBool();

        auto* stateItem = makeCell(configured ? "是" : "否");
        stateItem->setForeground(configured ? QColor("#5fd08a") : QColor("#e0a75a"));

        QList<QStandardItem*> rowItems;
        rowItems << makeCell(channel.value("id").toString())
                 << makeCell(channel.value("name").toString())
                 << stateItem
                 << makeCell(channel.value("masked").toString());
        channelModel->appendRow(rowItems);
    }
}

void DialogAutomation::applyScripts(const QJsonArray &scripts)
{
    scriptModel->removeRows(0, scriptModel->rowCount());

    for (const QJsonValue &value : scripts) {
        const QJsonObject script = value.toObject();

        QStringList osList;
        for (const QJsonValue &osValue : script.value("os").toArray())
            osList << osValue.toString();

        auto* destructiveItem = makeCell(script.value("destructive").toBool() ? "是" : "否");
        if (script.value("destructive").toBool())
            destructiveItem->setForeground(QColor("#e0a75a"));

        QList<QStandardItem*> rowItems;
        rowItems << makeCell(script.value("id").toString())
                 << makeCell(script.value("name").toString())
                 << makeCell(osList.join(", "))
                 << destructiveItem
                 << makeCell(script.value("description").toString());
        scriptModel->appendRow(rowItems);
    }
}

int DialogAutomation::currentRow(QTableView* view, QStandardItemModel* model) const
{
    const QModelIndexList selected = view->selectionModel()->selectedRows();
    if (selected.isEmpty())
        return -1;
    if (selected.first().row() < 0 || selected.first().row() >= model->rowCount())
        return -1;
    return selected.first().row();
}

void DialogAutomation::onRefresh()
{
    if (!profile)
        return;

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationRulesListAsync(*profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;

            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                safeThis->setStatus(error.isEmpty() ? "Failed to load automation rules." : error, true);
                return;
            }

            safeThis->applyRules(response.value("rules").toArray(), response.value("channels").toArray());
            safeThis->applyChannels(response.value("channels").toArray());
            safeThis->setStatus(QString("%1 rule(s) loaded.").arg(response.value("rules").toArray().size()), false);
        });
}

void DialogAutomation::onRefreshTemplates()
{
    if (!profile)
        return;

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationTemplatesListAsync(*profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;
            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                safeThis->setStatus(error.isEmpty() ? "Failed to load templates." : error, true);
                return;
            }
            safeThis->applyTemplates(response);
        });
}

void DialogAutomation::onRefreshChannels()
{
    if (!profile)
        return;

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationChannelsListAsync(*profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;
            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                safeThis->setStatus(error.isEmpty() ? "Failed to load channels." : error, true);
                return;
            }
            safeThis->applyChannels(response.value("channels").toArray());
        });
}

void DialogAutomation::onRefreshScripts()
{
    if (!profile)
        return;

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationScriptsListAsync(*profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;
            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                safeThis->setStatus(error.isEmpty() ? "Failed to load script library." : error, true);
                return;
            }
            safeThis->applyScripts(response.value("scripts").toArray());
        });
}

void DialogAutomation::onItemChanged(QStandardItem* item)
{
    if (updating || !item || item->column() != 5 || !profile)
        return;

    QStandardItem* idItem = rulesModel->item(item->row(), 0);
    if (!idItem)
        return;

    const QString id = idItem->text();
    const bool enabled = item->checkState() == Qt::Checked;

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationRuleUpdateAsync(QJsonObject{{"id", id}, {"enabled", enabled}}, *profile,
        [safeThis, id, enabled](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;

            const bool ok = success && response.value("ok").toBool();
            if (response.contains("rules"))
                safeThis->applyRules(response.value("rules").toArray(), response.value("channels").toArray());

            if (!ok) {
                const QString error = response.value("message").toString(message);
                safeThis->setStatus(error.isEmpty() ? "Failed to update rule." : error, true);
                return;
            }
            safeThis->setStatus(QString("规则 %1 已%2").arg(id, enabled ? "启用" : "禁用"), false);
        });
}

void DialogAutomation::onEditRule()
{
    showRuleEditor(currentRow(rulesView, rulesModel));
}

void DialogAutomation::showRuleEditor(int row)
{
    if (row < 0) {
        MessageError("Select a rule first.");
        return;
    }
    if (!profile)
        return;

    QPointer<DialogAutomation> safeThis = this;
    const QJsonObject rule = rulesData.at(row).toObject();

    auto* dialog = new DialogAutomationRule(profile, rule, artexTemplatesData, notifyTemplatesData, channelsData, this);
    if (dialog->exec() == QDialog::Accepted) {
        onRefresh();
        onRefreshTemplates();
        setStatus(QString("规则 %1 配置已更新").arg(rule.value("id").toString()), false);
    }
    dialog->deleteLater();
}

void DialogAutomation::showTemplateEditor(const QString &kind, const QString &id, const QJsonObject &existing)
{
    if (!profile)
        return;

    const bool isArtex = (kind == "artex");

    QDialog dialog(this);
    dialog.setWindowTitle(isArtex ? "ARTEX template" : "Notification template");
    dialog.resize(640, isArtex ? 620 : 460);

    auto* layout = new QVBoxLayout(&dialog);
    auto* grid = new QGridLayout();
    grid->setColumnStretch(1, 1);

    auto* idEdit   = new QLineEdit(id, &dialog);
    auto* nameEdit = new QLineEdit(existing.value("name").toString(), &dialog);
    auto* descEdit = new QLineEdit(existing.value("description").toString(), &dialog);
    if (!id.isEmpty())
        idEdit->setEnabled(false);

    int row = 0;
    grid->addWidget(new QLabel("ID", &dialog), row, 0);
    grid->addWidget(idEdit, row++, 1);
    grid->addWidget(new QLabel("名称", &dialog), row, 0);
    grid->addWidget(nameEdit, row++, 1);
    grid->addWidget(new QLabel("描述", &dialog), row, 0);
    grid->addWidget(descEdit, row++, 1);

    QTextEdit* goalEdit  = nullptr;
    QTextEdit* bodyEdit  = nullptr;
    QLineEdit* titleEdit = nullptr;
    QSpinBox*  workerSpin = nullptr;
    QSpinBox*  timeoutSpin = nullptr;
    QSpinBox*  cooldownSpin = nullptr;
    QLineEdit* dedupEdit = nullptr;
    QCheckBox* seedCheck = nullptr;
    QCheckBox* skipCheck = nullptr;

    if (isArtex) {
        goalEdit = new QTextEdit(&dialog);
        goalEdit->setAcceptRichText(false);
        goalEdit->setPlainText(existing.value("goal").toString());
        goalEdit->setMinimumHeight(160);
        grid->addWidget(new QLabel("提示词", &dialog), row, 0, Qt::AlignTop);
        grid->addWidget(goalEdit, row++, 1);

        workerSpin = new QSpinBox(&dialog);
        workerSpin->setRange(0, 64);
        workerSpin->setValue(existing.value("worker_count").toInt());
        grid->addWidget(new QLabel("Worker 数", &dialog), row, 0);
        grid->addWidget(workerSpin, row++, 1);

        timeoutSpin = new QSpinBox(&dialog);
        timeoutSpin->setRange(0, 86400);
        timeoutSpin->setValue(existing.value("timeout_seconds").toInt());
        grid->addWidget(new QLabel("超时（秒）", &dialog), row, 0);
        grid->addWidget(timeoutSpin, row++, 1);

        dedupEdit = new QLineEdit(existing.value("dedup_by").toString(), &dialog);
        dedupEdit->setPlaceholderText("computer / agent / none");
        grid->addWidget(new QLabel("去重维度", &dialog), row, 0);
        grid->addWidget(dedupEdit, row++, 1);

        cooldownSpin = new QSpinBox(&dialog);
        cooldownSpin->setRange(0, 604800);
        cooldownSpin->setValue(existing.value("cooldown_seconds").toInt());
        grid->addWidget(new QLabel("冷却（秒）", &dialog), row, 0);
        grid->addWidget(cooldownSpin, row++, 1);

        seedCheck = new QCheckBox("自动注入首个意图", &dialog);
        seedCheck->setChecked(existing.value("seed_first_intent").toBool());
        grid->addWidget(seedCheck, row++, 1);

        skipCheck = new QCheckBox("已有活动任务时跳过", &dialog);
        skipCheck->setChecked(existing.value("skip_if_running").toBool());
        grid->addWidget(skipCheck, row++, 1);
    } else {
        titleEdit = new QLineEdit(existing.value("title").toString(), &dialog);
        grid->addWidget(new QLabel("标题", &dialog), row, 0);
        grid->addWidget(titleEdit, row++, 1);

        bodyEdit = new QTextEdit(&dialog);
        bodyEdit->setAcceptRichText(false);
        bodyEdit->setPlainText(existing.value("body").toString());
        bodyEdit->setMinimumHeight(160);
        grid->addWidget(new QLabel("正文", &dialog), row, 0, Qt::AlignTop);
        grid->addWidget(bodyEdit, row++, 1);
    }

    layout->addLayout(grid);
    auto* buttons = new QDialogButtonBox(QDialogButtonBox::Save | QDialogButtonBox::Cancel, &dialog);
    QObject::connect(buttons, &QDialogButtonBox::accepted, &dialog, &QDialog::accept);
    QObject::connect(buttons, &QDialogButtonBox::rejected, &dialog, &QDialog::reject);
    layout->addWidget(buttons);

    if (dialog.exec() != QDialog::Accepted)
        return;

    const QString templateID = idEdit->text().trimmed();
    if (templateID.isEmpty()) {
        MessageError("Template id is required.");
        return;
    }

    QJsonObject templateData;
    templateData["name"]        = nameEdit->text().trimmed();
    templateData["description"] = descEdit->text().trimmed();

    if (isArtex) {
        templateData["goal"]              = goalEdit->toPlainText();
        templateData["worker_count"]      = workerSpin->value();
        templateData["timeout_seconds"]   = timeoutSpin->value();
        templateData["dedup_by"]          = dedupEdit->text().trimmed();
        templateData["cooldown_seconds"]  = cooldownSpin->value();
        templateData["seed_first_intent"] = seedCheck->isChecked();
        templateData["skip_if_running"]   = skipCheck->isChecked();
    } else {
        templateData["title"] = titleEdit->text().trimmed();
        templateData["body"]  = bodyEdit->toPlainText();
    }

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationTemplateSetAsync(kind, templateID, templateData, *profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;
            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                MessageError(error.isEmpty() ? "Failed to save template." : error);
                return;
            }
            safeThis->onRefreshTemplates();
            safeThis->setStatus("模板已保存", false);
        });
}

void DialogAutomation::onNewArtexTemplate()
{
    showTemplateEditor("artex", QString(), QJsonObject());
}

void DialogAutomation::onEditArtexTemplate()
{
    const int row = currentRow(artexView, artexModel);
    if (row < 0) {
        MessageError("Select a template first.");
        return;
    }
    const QString id = artexModel->item(row, 0)->text();

    // The list view only carries the summary, so fetch the full template.
    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationTemplatesListAsync(*profile,
        [safeThis, id](bool success, const QString &, const QJsonObject &response) {
            if (!safeThis || !success)
                return;
            const QJsonArray artex = response.value("artex").toArray();
            for (const QJsonValue &value : artex) {
                const QJsonObject tpl = value.toObject();
                if (tpl.value("id").toString() == id) {
                    safeThis->showTemplateEditor("artex", id, tpl);
                    return;
                }
            }
        });
}

void DialogAutomation::onDeleteArtexTemplate()
{
    const int row = currentRow(artexView, artexModel);
    if (row < 0) {
        MessageError("Select a template first.");
        return;
    }
    const QString id = artexModel->item(row, 0)->text();

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationTemplateDeleteAsync("artex", id, *profile,
        [safeThis, id](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;
            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                MessageError(error.isEmpty() ? "Failed to delete template." : error);
                return;
            }
            safeThis->onRefreshTemplates();
            safeThis->setStatus(QString("模板 %1 已删除").arg(id), false);
        });
}

void DialogAutomation::onNewNotifyTemplate()
{
    showTemplateEditor("notify", QString(), QJsonObject());
}

void DialogAutomation::onEditNotifyTemplate()
{
    const int row = currentRow(notifyView, notifyModel);
    if (row < 0) {
        MessageError("Select a template first.");
        return;
    }
    const QString id = notifyModel->item(row, 0)->text();

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationTemplatesListAsync(*profile,
        [safeThis, id](bool success, const QString &, const QJsonObject &response) {
            if (!safeThis || !success)
                return;
            const QJsonArray notify = response.value("notify").toArray();
            for (const QJsonValue &value : notify) {
                const QJsonObject tpl = value.toObject();
                if (tpl.value("id").toString() == id) {
                    safeThis->showTemplateEditor("notify", id, tpl);
                    return;
                }
            }
        });
}

void DialogAutomation::onDeleteNotifyTemplate()
{
    const int row = currentRow(notifyView, notifyModel);
    if (row < 0) {
        MessageError("Select a template first.");
        return;
    }
    const QString id = notifyModel->item(row, 0)->text();

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationTemplateDeleteAsync("notify", id, *profile,
        [safeThis, id](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;
            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                MessageError(error.isEmpty() ? "Failed to delete template." : error);
                return;
            }
            safeThis->onRefreshTemplates();
            safeThis->setStatus(QString("模板 %1 已删除").arg(id), false);
        });
}

void DialogAutomation::onConfigureChannel()
{
    const int row = currentRow(channelView, channelModel);
    if (row < 0) {
        MessageError("Select a channel first.");
        return;
    }
    showChannelEditor(channelModel->item(row, 0)->text(),
                      channelModel->item(row, 1)->text(),
                      "SendKey");
}

void DialogAutomation::showChannelEditor(const QString &channelId, const QString &channelName, const QString &secretLabel)
{
    if (!profile)
        return;

    QDialog dialog(this);
    dialog.setWindowTitle(QString("Configure %1").arg(channelName));
    dialog.resize(520, 200);

    auto* keyEdit = new QLineEdit(&dialog);
    keyEdit->setEchoMode(QLineEdit::Password);
    keyEdit->setPlaceholderText("留空表示不修改");

    auto* baseEdit = new QLineEdit(&dialog);
    baseEdit->setPlaceholderText("留空表示不修改（仅测试用）");

    auto* grid = new QGridLayout();
    grid->addWidget(new QLabel(QString("%1（留空不修改）:").arg(secretLabel), &dialog), 0, 0);
    grid->addWidget(keyEdit, 0, 1);
    grid->addWidget(new QLabel("API base（留空不修改）:"), 1, 0);
    grid->addWidget(baseEdit, 1, 1);

    auto* layout = new QVBoxLayout(&dialog);
    layout->addLayout(grid);
    auto* buttons = new QDialogButtonBox(QDialogButtonBox::Save | QDialogButtonBox::Cancel, &dialog);
    QObject::connect(buttons, &QDialogButtonBox::accepted, &dialog, &QDialog::accept);
    QObject::connect(buttons, &QDialogButtonBox::rejected, &dialog, &QDialog::reject);
    layout->addWidget(buttons);

    if (dialog.exec() != QDialog::Accepted)
        return;

    QPointer<DialogAutomation> safeThis = this;
    HttpReqAutomationChannelSetAsync(channelId, keyEdit->text().trimmed(), baseEdit->text().trimmed(), *profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;
            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                MessageError(error.isEmpty() ? "Failed to update channel." : error);
                return;
            }
            safeThis->applyChannels(response.value("channels").toArray());
            safeThis->setStatus("渠道配置已更新", false);
        });
}
