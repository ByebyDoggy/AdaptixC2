#include <UI/Dialogs/DialogAutomationRule.h>
#include <Client/AuthProfile.h>
#include <Client/Requestor.h>
#include <QPointer>

DialogAutomationRule::DialogAutomationRule(AuthProfile* authProfile, const QJsonObject &ruleData,
                                           const QJsonArray &artexTemplates, const QJsonArray &notifyTemplates,
                                           const QJsonArray &channels, QWidget* parent)
    : QDialog(parent), profile(authProfile), rule(ruleData)
{
    createUI(artexTemplates, notifyTemplates, channels);

    connect(saveButton,   &QPushButton::clicked, this, &DialogAutomationRule::onSave);
    connect(cancelButton, &QPushButton::clicked, this, &QDialog::reject);
}

DialogAutomationRule::~DialogAutomationRule() = default;

void DialogAutomationRule::createUI(const QJsonArray &artexTemplates, const QJsonArray &notifyTemplates, const QJsonArray &channels)
{
    this->setWindowTitle(QString("Rule configuration · %1").arg(rule.value("id").toString()));
    this->resize(720, 760);

    /// Basic
    auto* basicGroup = new QGroupBox("Basic", this);
    auto* basicLayout = new QGridLayout(basicGroup);
    basicLayout->setColumnStretch(1, 1);

    nameEdit = new QLineEdit(rule.value("name").toString(), basicGroup);
    descEdit = new QLineEdit(rule.value("description").toString(), basicGroup);

    triggerCombo = new QComboBox(basicGroup);
    triggerCombo->addItem("Agent 上线", "agent.online");
    const QString trigger = rule.value("trigger").toString();
    const int triggerIndex = triggerCombo->findData(trigger.isEmpty() ? "agent.online" : trigger);
    triggerCombo->setCurrentIndex(triggerIndex >= 0 ? triggerIndex : 0);

    orderSpin = new QSpinBox(basicGroup);
    orderSpin->setRange(0, 100000);
    orderSpin->setValue(rule.value("order").toInt());

    delaySpin = new QSpinBox(basicGroup);
    delaySpin->setRange(0, 86400);
    delaySpin->setSuffix(" s");
    delaySpin->setValue(rule.value("delay_seconds").toInt());

    enabledCheck = new QCheckBox("Enabled", basicGroup);
    enabledCheck->setChecked(rule.value("enabled").toBool(true));

    int row = 0;
    basicLayout->addWidget(new QLabel("ID", basicGroup), row, 0);
    auto* idLabel = new QLabel(rule.value("id").toString(), basicGroup);
    idLabel->setTextInteractionFlags(Qt::TextSelectableByMouse);
    basicLayout->addWidget(idLabel, row++, 1);

    basicLayout->addWidget(new QLabel("名称", basicGroup), row, 0);
    basicLayout->addWidget(nameEdit, row++, 1);
    basicLayout->addWidget(new QLabel("描述", basicGroup), row, 0);
    basicLayout->addWidget(descEdit, row++, 1);
    basicLayout->addWidget(new QLabel("触发时机", basicGroup), row, 0);
    basicLayout->addWidget(triggerCombo, row++, 1);
    basicLayout->addWidget(new QLabel("执行顺序", basicGroup), row, 0);
    basicLayout->addWidget(orderSpin, row++, 1);
    basicLayout->addWidget(new QLabel("延迟", basicGroup), row, 0);
    basicLayout->addWidget(delaySpin, row++, 1);
    basicLayout->addWidget(enabledCheck, row++, 1);

    /// Actions
    auto* actionsGroup = new QGroupBox("Actions", this);
    auto* actionsLayout = new QGridLayout(actionsGroup);
    actionsLayout->setColumnStretch(1, 1);

    commandsEdit = new QTextEdit(actionsGroup);
    commandsEdit->setAcceptRichText(false);
    commandsEdit->setPlaceholderText("每行一条命令，例如：shell id");
    commandsEdit->setPlainText(rule.value("commands").toVariant().toStringList().join("\n"));
    commandsEdit->setMinimumHeight(110);

    artexCombo = new QComboBox(actionsGroup);
    artexCombo->addItem("(不使用)", "");
    for (const QJsonValue &value : artexTemplates) {
        const QJsonObject tpl = value.toObject();
        artexCombo->addItem(tpl.value("name").toString(), tpl.value("id").toString());
    }
    const QString artexID = rule.value("artex_template_id").toString();
    artexCombo->setCurrentIndex(qMax(0, artexCombo->findData(artexID)));

    notifyChannelCombo = new QComboBox(actionsGroup);
    notifyChannelCombo->addItem("(不使用)", "");
    for (const QJsonValue &value : channels) {
        const QJsonObject channel = value.toObject();
        const QString label = channel.value("configured").toBool()
                                  ? channel.value("name").toString()
                                  : QString("%1（未配置）").arg(channel.value("name").toString());
        notifyChannelCombo->addItem(label, channel.value("id").toString());
    }
    const QString channelID = rule.value("notify_channel_id").toString();
    notifyChannelCombo->setCurrentIndex(qMax(0, notifyChannelCombo->findData(channelID)));

    notifyTemplateCombo = new QComboBox(actionsGroup);
    notifyTemplateCombo->addItem("(默认内容)", "");
    for (const QJsonValue &value : notifyTemplates) {
        const QJsonObject tpl = value.toObject();
        notifyTemplateCombo->addItem(tpl.value("name").toString(), tpl.value("id").toString());
    }
    const QString notifyTemplateID = rule.value("notify_template_id").toString();
    notifyTemplateCombo->setCurrentIndex(qMax(0, notifyTemplateCombo->findData(notifyTemplateID)));

    scriptLabel = new QLabel(actionsGroup);
    const QString scriptID = rule.value("script").toString();
    const QStringList scriptArgs = rule.value("script_args").toVariant().toStringList();
    scriptLabel->setText(scriptID.isEmpty() ? "—" : QString("%1 %2").arg(scriptID, scriptArgs.join(" ")).trimmed());
    scriptLabel->setTextInteractionFlags(Qt::TextSelectableByMouse);
    scriptLabel->setToolTip("脚本引用在 profile.yaml 中定义，客户端不可修改");

    persistLabel = new QLabel(actionsGroup);
    const bool hasPersist = rule.value("actions").toObject().value("persist").toBool();
    persistLabel->setText(hasPersist ? "已配置（profile.yaml 定义）" : "—");

    row = 0;
    actionsLayout->addWidget(new QLabel("命令", actionsGroup), row, 0, Qt::AlignTop);
    actionsLayout->addWidget(commandsEdit, row++, 1);
    actionsLayout->addWidget(new QLabel("ARTEX 模板", actionsGroup), row, 0);
    actionsLayout->addWidget(artexCombo, row++, 1);
    actionsLayout->addWidget(new QLabel("通知渠道", actionsGroup), row, 0);
    actionsLayout->addWidget(notifyChannelCombo, row++, 1);
    actionsLayout->addWidget(new QLabel("通知模板", actionsGroup), row, 0);
    actionsLayout->addWidget(notifyTemplateCombo, row++, 1);
    actionsLayout->addWidget(new QLabel("脚本", actionsGroup), row, 0);
    actionsLayout->addWidget(scriptLabel, row++, 1);
    actionsLayout->addWidget(new QLabel("持久化", actionsGroup), row, 0);
    actionsLayout->addWidget(persistLabel, row++, 1);

    auto* matchGroup = new QGroupBox("Match (profile.yaml)", this);
    auto* matchLayout = new QGridLayout(matchGroup);
    QStringList matchParts;
    if (!rule.value("agent").toString().isEmpty())    matchParts << QString("agent: %1").arg(rule.value("agent").toString());
    if (!rule.value("os").toString().isEmpty())       matchParts << QString("os: %1").arg(rule.value("os").toString());
    if (!rule.value("listener").toString().isEmpty()) matchParts << QString("listener: %1").arg(rule.value("listener").toString());
    matchLayout->addWidget(new QLabel(matchParts.isEmpty() ? "all" : matchParts.join("  ·  "), matchGroup), 0, 0);

    statusLabel = new QLabel("", this);
    statusLabel->setWordWrap(true);

    saveButton   = new QPushButton("Save", this);
    cancelButton = new QPushButton("Cancel", this);

    auto* buttons = new QHBoxLayout();
    buttons->addWidget(statusLabel, 1);
    buttons->addWidget(saveButton);
    buttons->addWidget(cancelButton);

    auto* layout = new QVBoxLayout(this);
    layout->addWidget(basicGroup);
    layout->addWidget(actionsGroup, 1);
    layout->addWidget(matchGroup);
    layout->addLayout(buttons);

    this->setLayout(layout);
}

QJsonObject DialogAutomationRule::buildPatch() const
{
    QJsonObject patch;
    patch["id"]                 = rule.value("id").toString();
    patch["name"]               = nameEdit->text().trimmed();
    patch["description"]        = descEdit->text().trimmed();
    patch["trigger"]            = triggerCombo->currentData().toString();
    patch["order"]              = orderSpin->value();
    patch["delay_seconds"]      = delaySpin->value();
    patch["enabled"]            = enabledCheck->isChecked();
    patch["commands"]           = QJsonArray::fromStringList(commandsEdit->toPlainText().split('\n', Qt::SkipEmptyParts));
    patch["artex_template_id"]  = artexCombo->currentData().toString();
    patch["notify_channel_id"]  = notifyChannelCombo->currentData().toString();
    patch["notify_template_id"] = notifyTemplateCombo->currentData().toString();
    return patch;
}

void DialogAutomationRule::onSave()
{
    if (!profile)
        return;

    saveButton->setEnabled(false);
    statusLabel->setText("Saving...");
    statusLabel->setStyleSheet("color:#5fd08a;");

    QPointer<DialogAutomationRule> safeThis = this;
    HttpReqAutomationRuleUpdateAsync(buildPatch(), *profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;

            safeThis->saveButton->setEnabled(true);

            if (!success || !response.value("ok").toBool()) {
                const QString error = response.value("message").toString(message);
                safeThis->statusLabel->setText(error.isEmpty() ? "Failed to save rule." : error);
                safeThis->statusLabel->setStyleSheet("color:#e05a5a;");
                return;
            }

            safeThis->accept();
        });
}
